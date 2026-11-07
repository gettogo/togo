package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

// fakeDaemon answers hello and then hands each call to reply. A nil result
// with a non-zero code answers with that error code.
type fakeDaemon struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Method string
	Params json.RawMessage
}

func startFakeDaemon(t *testing.T, sock string, reply func(method string, params json.RawMessage) (any, int)) *fakeDaemon {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	d := &fakeDaemon{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn, reply)
		}
	}()
	return d
}

func (d *fakeDaemon) serve(conn net.Conn, reply func(string, json.RawMessage) (any, int)) {
	defer conn.Close()
	for {
		frame, err := rpc.ReadFrame(conn)
		if err != nil {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(frame, &req) != nil {
			return
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if req.Method == "hello" {
			resp["result"] = map[string]any{"protocol": 1}
		} else {
			d.mu.Lock()
			d.calls = append(d.calls, fakeCall{req.Method, req.Params})
			d.mu.Unlock()
			// A notification first, which the client must skip.
			note, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{}})
			_ = rpc.WriteFrame(conn, note)
			res, code := reply(req.Method, req.Params)
			if code != 0 {
				resp["error"] = map[string]any{"code": code, "message": "x", "data": map[string]any{"echo": "SECRET"}}
			} else {
				resp["result"] = res
			}
		}
		b, _ := json.Marshal(resp)
		if rpc.WriteFrame(conn, b) != nil {
			return
		}
	}
}

func (d *fakeDaemon) recorded() []fakeCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]fakeCall(nil), d.calls...)
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "adapters", "claude", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runWith(t *testing.T, env map[string]string, stdin []byte, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runContext(context.Background(), args, env, bytes.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func hookLog(t *testing.T, home string) string {
	b, _ := os.ReadFile(filepath.Join(home, "log", "hook.log"))
	return string(b)
}

func TestHookSendsMappedEvent(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	code, out, _ := runWith(t, env, fixtureBytes(t, "pre_tool_use.json"), "hook", "claude", "PreToolUse")
	if code != 0 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.hook" || !strings.Contains(string(calls[0].Params), `"tool":"Bash"`) {
		t.Fatalf("calls %+v", calls)
	}
	if strings.Contains(string(calls[0].Params), "SECRET") || strings.Contains(hookLog(t, home), "SECRET") {
		t.Fatal("tool input reached the daemon or the log")
	}
}

func TestToolInputNeverInHookLog(t *testing.T) {
	home, sock := tapHome(t)
	startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return nil, -32602 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	runWith(t, env, fixtureBytes(t, "pre_tool_use.json"), "hook", "claude", "PreToolUse")
	runWith(t, env, []byte(`{"tool_input":"SECRET`), "hook", "claude", "PreToolUse")
	log := hookLog(t, home)
	if log == "" || strings.Contains(log, "SECRET") || strings.Contains(log, "curl") {
		t.Fatalf("log %q", log)
	}
}

func TestMissingTogoSessionIsNoOpForNonBlocking(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	env := map[string]string{"TOGO_HOME": home}
	for _, ev := range []string{"PreToolUse", "PermissionRequest"} {
		if code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request.json"), "hook", "claude", ev); code != 0 || out != "" {
			t.Fatalf("%s: code %d out %q", ev, code, out)
		}
	}
	if n := len(d.recorded()); n != 0 {
		t.Fatalf("%d daemon calls without a session", n)
	}
}

func TestBlockingPrintsAllowOnApprove(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) {
		return map[string]any{"decision": "Allow", "reason": "answered"}, 0
	})
	env := map[string]string{"TOGO_HOME": home}
	code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request.json"), "hook", "claude", "PermissionRequest", "--block")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior string `json:"behavior"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Fatalf("out %q err %v", out, err)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.ask" || !strings.Contains(string(calls[0].Params), `"timeoutSec":540`) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestBlockingTimeoutPrintsNothing(t *testing.T) {
	home, sock := tapHome(t)
	startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) {
		return map[string]any{"decision": nil, "reason": "timeout"}, 0
	})
	env := map[string]string{"TOGO_HOME": home, "TOGO_HOOK_BLOCK": "1"}
	if code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request.json"), "hook", "claude", "PermissionRequest"); code != 0 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestBlockingDaemonDownExitsZeroSilently(t *testing.T) {
	home, _ := tapHome(t)
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	start := time.Now()
	code, out, errOut := runWith(t, env, fixtureBytes(t, "permission_request.json"), "hook", "--block", "claude", "PermissionRequest")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("hook waited for a missing daemon")
	}
}

func TestHookNeverFailsOnBadInput(t *testing.T) {
	home, _ := tapHome(t)
	env := map[string]string{"TOGO_HOME": home}
	for _, args := range [][]string{{"hook"}, {"hook", "nope", "Stop"}, {"hook", "claude", "Bogus"}} {
		if code, out, _ := runWith(t, env, []byte(`{}`), args...); code != 0 || out != "" {
			t.Fatalf("%v: code %d out %q", args, code, out)
		}
	}
}

func TestHookStdinNeverClosedDoesNotHang(t *testing.T) {
	home, _ := tapHome(t)
	r, w := ioPipe(t)
	defer w.Close()
	start := time.Now()
	var out, errb bytes.Buffer
	code := runContext(context.Background(), []string{"hook", "claude", "Stop"}, map[string]string{"TOGO_HOME": home}, r, &out, &errb)
	if code != 0 || time.Since(start) > hookStdinWait+2*time.Second {
		t.Fatalf("code %d after %v", code, time.Since(start))
	}
}

func ioPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, w
}

func TestStatuslinePrintsModelAndContext(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	code, out, _ := runWith(t, env, fixtureBytes(t, "statusline.json"), "hook", "claude", "statusline")
	if code != 0 || out != "Opus 8%\n" {
		t.Fatalf("code %d out %q", code, out)
	}
	if calls := d.recorded(); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"costUsd":0.01234`) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestCodexAdapterRegistered(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	payload := []byte(`{"session_id":"x","cwd":"/tmp","hook_event_name":"Stop","last_assistant_message":"ok"}`)
	if code, _, _ := runWith(t, env, payload, "hook", "codex", "Stop"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if calls := d.recorded(); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"agent":"codex"`) {
		t.Fatalf("calls %+v", calls)
	}
}

// Claude and Codex hooks carry their redacted payload and hook name, so the
// harness feeds see more than the summary.
func TestHookForwardsPayload(t *testing.T) {
	cases := []struct {
		agent, event string
		payload      []byte
		want         []string
	}{
		{"claude", "PreToolUse", fixtureBytes(t, "pre_tool_use.json"), []string{`"hook":"PreToolUse"`, `"tool_use_id":"toolu_01ABC123"`}},
		{"claude", "SubagentStart", []byte(`{"session_id":"a","cwd":"/w","agent_id":"ag1","agent_type":"Explore"}`), []string{`"hook":"SubagentStart"`, `"agent_id":"ag1"`}},
		{"claude", "statusline", fixtureBytes(t, "statusline.json"), []string{`"hook":"statusline"`, `"payload":{`}},
		{"codex", "Stop", []byte(`{"session_id":"x","cwd":"/tmp","hook_event_name":"Stop","last_assistant_message":"ok","turn_id":"t1"}`), []string{`"hook":"Stop"`, `"turn_id":"t1"`}},
	}
	for _, c := range cases {
		home, sock := tapHome(t)
		d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
		env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa", "TOGO_AGENT": c.agent}
		if code, _, _ := runWith(t, env, c.payload, "hook", c.agent, c.event); code != 0 {
			t.Fatalf("%s %s: code %d", c.agent, c.event, code)
		}
		calls := d.recorded()
		if len(calls) != 1 {
			t.Fatalf("%s %s: calls %+v", c.agent, c.event, calls)
		}
		p := string(calls[0].Params)
		for _, w := range c.want {
			if !strings.Contains(p, w) {
				t.Errorf("%s %s: missing %s in %s", c.agent, c.event, w, p)
			}
		}
		if strings.Contains(p, "SECRETSECRET") {
			t.Errorf("%s %s: secret forwarded", c.agent, c.event)
		}
	}
}
