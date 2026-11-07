package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func chainEnv(home string) map[string]string {
	return map[string]string{"TOGO_HOME": home, "TOGO_SESSION": "s_aaaaaaaaaa", "PATH": "/usr/bin:/bin"}
}

func TestStatuslineThenPrintsTheUsersOutputUnchanged(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	payload := fixtureBytes(t, "statusline.json")
	// The user's command echoes its stdin's model through, with its own
	// formatting and no trailing newline.
	user := `printf '[%s]' "$(cat | tr -d '\n' | wc -c | tr -d ' ')"`
	code, out, _ := runWith(t, chainEnv(home), payload, "hook", "claude", "statusline", "--then", user)
	want := "[" + itoa(len(strings.TrimRight(string(payload), "\n"))) + "]"
	if code != 0 || out != want {
		t.Fatalf("code %d out %q, want %q", code, out, want)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(d.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	calls := d.recorded()
	if len(calls) != 1 || calls[0].Method != "local.hook" || !strings.Contains(string(calls[0].Params), `"hook":"statusline"`) {
		t.Fatalf("calls %+v", calls)
	}
}

func TestStatuslineThenPassesTheExitCode(t *testing.T) {
	home, _ := tapHome(t) // no daemon: the report fails, the user's line still prints
	code, out, _ := runWith(t, chainEnv(home), fixtureBytes(t, "statusline.json"), "hook", "claude", "statusline", "--then", "echo mine; exit 3")
	if code != 3 || out != "mine\n" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestStatuslineThenTimesOut(t *testing.T) {
	old := statuslineThenWait
	statuslineThenWait = 200 * time.Millisecond
	t.Cleanup(func() { statuslineThenWait = old })
	home, _ := tapHome(t)
	start := time.Now()
	code, _, _ := runWith(t, chainEnv(home), []byte(`{}`), "hook", "claude", "statusline", "--then", "sleep 5")
	if code == 0 || time.Since(start) > 3*time.Second {
		t.Fatalf("code %d after %v", code, time.Since(start))
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
