package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func okDaemon(t *testing.T, sock string, decision any) *fakeDaemon {
	t.Helper()
	return startFakeDaemon(t, sock, func(method string, _ json.RawMessage) (any, int) {
		if method == "local.ask" {
			return map[string]any{"decision": decision, "reason": "answered"}, 0
		}
		return map[string]any{}, 0
	})
}

func TestCopilotHookForwardsNormalizedPayload(t *testing.T) {
	home, sock := tapHome(t)
	d := okDaemon(t, sock, nil)
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa", "TOGO_AGENT": "copilot"}
	in := `{"timestamp":1,"cwd":"/w","toolName":"bash","toolArgs":"{\"command\":\"ls\"}"}`
	if code, out, _ := runWith(t, env, []byte(in), "hook", "copilot", "preToolUse"); code != 0 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.hook" {
		t.Fatalf("calls %+v", calls)
	}
	var f struct {
		Agent   string         `json:"agent"`
		Event   string         `json:"event"`
		Hook    string         `json:"hook"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(calls[0].Params, &f); err != nil {
		t.Fatal(err)
	}
	if f.Agent != "copilot" || f.Event != "tool" || f.Hook != "PreToolUse" || f.Payload["tool_name"] != "bash" {
		t.Fatalf("forward %+v", f)
	}
}

// Copilot runs the hooks in .claude/settings*.json. A `togo hook claude`
// call from inside a Copilot session must not report to the daemon, ask
// the phone, or print a Claude decision into Copilot.
func TestClaudeHookInsideCopilotSessionDoesNothing(t *testing.T) {
	home, sock := tapHome(t)
	d := okDaemon(t, sock, "Allow")
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa", "TOGO_AGENT": "copilot"}
	for _, ev := range []string{"PreToolUse", "Stop"} {
		if code, out, _ := runWith(t, env, fixtureBytes(t, "pre_tool_use.json"), "hook", "claude", ev); code != 0 || out != "" {
			t.Fatalf("%s: code %d out %q", ev, code, out)
		}
	}
	if code, out, _ := runWith(t, env, fixtureBytes(t, "permission_request.json"), "hook", "claude", "PermissionRequest", "--block"); code != 0 || out != "" {
		t.Fatalf("blocking: code %d out %q", code, out)
	}
	if n := len(d.recorded()); n != 0 {
		t.Fatalf("%d daemon calls from a foreign hook", n)
	}
	if log := hookLog(t, home); !strings.Contains(log, "session runs copilot") || strings.Contains(log, "curl") {
		t.Fatalf("log %q", log)
	}
	// The same hook in a Claude session, or one started before sessions
	// named their agent, still reports.
	env["TOGO_AGENT"] = "claude"
	runWith(t, env, fixtureBytes(t, "pre_tool_use.json"), "hook", "claude", "PreToolUse")
	delete(env, "TOGO_AGENT")
	runWith(t, env, fixtureBytes(t, "pre_tool_use.json"), "hook", "claude", "PreToolUse")
	if n := len(d.recorded()); n != 2 {
		t.Fatalf("%d daemon calls, want 2", n)
	}
}

func TestQwenAndCopilotBlockingAnswers(t *testing.T) {
	home, sock := tapHome(t)
	d := okDaemon(t, sock, "Allow")
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	req := `{"session_id":"qw","hook_event_name":"PermissionRequest","tool_name":"run_shell_command","tool_input":{"command":"make"}}`
	_, out, _ := runWith(t, env, []byte(req), "hook", "qwen", "PermissionRequest", "--block")
	if !strings.Contains(out, `"behavior":"allow"`) || !strings.Contains(out, `"hookEventName":"PermissionRequest"`) {
		t.Fatalf("qwen out %q", out)
	}
	_, out, _ = runWith(t, env, []byte(`{"toolName":"bash","toolArgs":"{\"command\":\"make\"}"}`), "hook", "copilot", "permissionRequest", "--block")
	if strings.TrimSpace(out) != `{"permissionDecision":"allow"}` {
		t.Fatalf("copilot out %q", out)
	}
	// Droid has no hook that can answer, so it never waits on the phone.
	_, out, _ = runWith(t, env, []byte(`{"session_id":"dr","tool_name":"Execute"}`), "hook", "droid", "PreToolUse", "--block")
	if out != "" {
		t.Fatalf("droid out %q", out)
	}
	asks := 0
	for _, c := range d.recorded() {
		if c.Method == "local.ask" {
			asks++
			if !strings.Contains(string(c.Params), "make") {
				t.Fatalf("ask %s", c.Params)
			}
		}
	}
	if asks != 2 {
		t.Fatalf("%d asks, want 2", asks)
	}
}
