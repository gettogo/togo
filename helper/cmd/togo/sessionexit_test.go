package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionExitSendsSignal(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	code, out, errOut := runWith(t, env, nil, "session-exit", "3")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.signal" {
		t.Fatalf("calls %+v", calls)
	}
	var got map[string]any
	if err := json.Unmarshal(calls[0].Params, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"sessionId": "s_aaaaaaaaaa", "kind": "exit", "code": float64(3)}
	if len(got) != len(want) || got["sessionId"] != want["sessionId"] || got["kind"] != want["kind"] || got["code"] != want["code"] {
		t.Fatalf("params %s, want %v", calls[0].Params, want)
	}

	if code, _, _ := runWith(t, env, nil, "session-exit", "0"); code != 0 {
		t.Fatalf("code 0: exit %d", code)
	}
	if calls := d.recorded(); len(calls) != 2 || !strings.Contains(string(calls[1].Params), `"code":0`) {
		t.Fatalf("exit 0 calls %+v", calls)
	}
}

func TestSessionExitBadInputIsSilent(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	withSession := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	for name, c := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"no session":     {map[string]string{"TOGO_HOME": home}, []string{"session-exit", "1"}},
		"bad session":    {map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "x; rm"}, []string{"session-exit", "1"}},
		"code 300":       {withSession, []string{"session-exit", "300"}},
		"negative":       {withSession, []string{"session-exit", "-1"}},
		"signed":         {withSession, []string{"session-exit", "+1"}},
		"not a number":   {withSession, []string{"session-exit", "x"}},
		"no code":        {withSession, []string{"session-exit"}},
		"two codes":      {withSession, []string{"session-exit", "1", "2"}},
		"leading zeroes": {withSession, []string{"session-exit", "0000000000000000001"}},
	} {
		code, out, errOut := runWith(t, c.env, nil, c.args...)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("%s: code %d stdout %q stderr %q", name, code, out, errOut)
		}
	}
	if n := len(d.recorded()); n != 0 {
		t.Fatalf("%d daemon calls for bad input", n)
	}
}

func TestSessionExitDaemonDownIsSilent(t *testing.T) {
	home, _ := tapHome(t)
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	start := time.Now()
	code, out, errOut := runWith(t, env, nil, "session-exit", "1")
	if code != 0 || out != "" || errOut != "" || time.Since(start) > 3*time.Second {
		t.Fatalf("code %d stdout %q stderr %q after %v", code, out, errOut, time.Since(start))
	}
}

func TestSessionExitHasNoUsageLine(t *testing.T) {
	if strings.Contains(usage, "session-exit") {
		t.Fatal("usage names the internal session-exit command")
	}
}
