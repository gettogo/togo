package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAskExitCodes(t *testing.T) {
	cases := []struct {
		name   string
		reply  any
		args   []string
		code   int
		stdout string
	}{
		{"approve", map[string]any{"decision": "Yes", "reason": "answered"}, nil, 0, "Yes\n"},
		{"deny", map[string]any{"decision": "deny", "reason": "answered"}, nil, 1, "No\n"},
		{"no", map[string]any{"decision": "No", "reason": "answered"}, nil, 1, "No\n"},
		{"option", map[string]any{"decision": "prod", "reason": "answered"}, []string{"--options", "staging,prod"}, 0, "prod\n"},
		{"timeout", map[string]any{"decision": nil, "reason": "timeout"}, nil, 3, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, sock := tapHome(t)
			d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return c.reply, 0 })
			args := append([]string{"ask", "Deploy now?", "--timeout", "5"}, c.args...)
			code, out, _ := runWith(t, map[string]string{"TOGO_HOME": home}, nil, args...)
			if code != c.code || out != c.stdout {
				t.Fatalf("code %d out %q, want %d %q", code, out, c.code, c.stdout)
			}
			if calls := d.recorded(); len(calls) != 1 || calls[0].Method != "local.ask" {
				t.Fatalf("calls %+v", calls)
			}
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		home, _ := tapHome(t)
		if code, _, _ := runWith(t, map[string]string{"TOGO_HOME": home}, nil, "ask", "Deploy?"); code != 4 {
			t.Fatalf("code %d, want 4", code)
		}
	})
}

func TestAskRejectsBadArguments(t *testing.T) {
	home, _ := tapHome(t)
	env := map[string]string{"TOGO_HOME": home}
	for _, args := range [][]string{{"ask"}, {"ask", "q", "--timeout", "0"}, {"ask", "q", "--options", "1,2,3,4,5,6,7,8,9,10"}} {
		if code, _, _ := runWith(t, env, nil, args...); code != exitUsage {
			t.Fatalf("%v: code %d", args, code)
		}
	}
}

func TestNotifyStrictExit4WhenUnreachable(t *testing.T) {
	home, _ := tapHome(t)
	env := map[string]string{"TOGO_HOME": home}
	if code, _, _ := runWith(t, env, nil, "notify", "build done", "--strict"); code != 4 {
		t.Fatalf("strict code %d, want 4", code)
	}
	if code, _, _ := runWith(t, env, nil, "notify", "build done"); code != 0 {
		t.Fatalf("non-strict code %d, want 0", code)
	}
}

func TestNotifyStrictNotDelivered(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{"delivered": false}, 0 })
	env := map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa"}
	if code, _, _ := runWith(t, env, nil, "notify", "tests passed", "--title", "CI", "--strict"); code != 4 {
		t.Fatalf("code %d, want 4", code)
	}
	p := string(d.recorded()[0].Params)
	if !strings.Contains(p, `"title":"CI"`) || !strings.Contains(p, `"body":"tests passed"`) || !strings.Contains(p, `"sessionId":"s_aaaaaaaaaa"`) {
		t.Fatalf("params %s", p)
	}
}

func TestJobCommands(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(_ string, p json.RawMessage) (any, int) {
		if strings.Contains(string(p), "j_bbbbbbbbbb") {
			return nil, -32001
		}
		return map[string]any{"jobId": "j_aaaaaaaaaa"}, 0
	})
	env := map[string]string{"TOGO_HOME": home}
	if code, out, _ := runWith(t, env, nil, "job", "start", "nightly", "build"); code != 0 || out != "j_aaaaaaaaaa\n" {
		t.Fatalf("start: %d %q", code, out)
	}
	if code, _, _ := runWith(t, env, nil, "job", "update", "j_aaaaaaaaaa", "--progress", "40", "--message", "linking"); code != 0 {
		t.Fatalf("update: %d", code)
	}
	if code, _, _ := runWith(t, env, nil, "job", "end", "j_aaaaaaaaaa", "--status", "ok"); code != 0 {
		t.Fatalf("end: %d", code)
	}
	if code, _, errOut := runWith(t, env, nil, "job", "end", "j_bbbbbbbbbb", "--status", "ok"); code != 1 || strings.Contains(errOut, "SECRET") {
		t.Fatalf("unknown job: %d %q", code, errOut)
	}
	calls := d.recorded()
	if !strings.Contains(string(calls[0].Params), `"name":"nightly build"`) || !strings.Contains(string(calls[1].Params), `"progress":40`) {
		t.Fatalf("calls %+v", calls)
	}
	for _, args := range [][]string{{"job"}, {"job", "end", "j_aaaaaaaaaa"}, {"job", "update", "x", "--progress", "101"}, {"job", "pause", "x"}} {
		if code, _, _ := runWith(t, env, nil, args...); code != exitUsage {
			t.Fatalf("%v: code %d", args, code)
		}
	}
	home2, _ := tapHome(t)
	if code, _, _ := runWith(t, map[string]string{"TOGO_HOME": home2}, nil, "job", "start", "x"); code != 4 {
		t.Fatalf("unreachable start: %d", code)
	}
}
