package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/hooks"
)

// The status line setup installs runs the user's own command and prints
// exactly what it printed before, while the payload also reaches Togo.
func TestInstalledStatusLineChainPrintsTheUsersOutput(t *testing.T) {
	home, sock := tapHome(t)
	d := startFakeDaemon(t, sock, func(string, json.RawMessage) (any, int) { return map[string]any{}, 0 })
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "statusline command.sh")
	body := "#!/bin/sh\ninput=$(cat)\nprintf '\\033[2m%s\\033[0m | %s bytes\\n' \"$(basename \"$PWD\")\" \"$(printf %s \"$input\" | wc -c | tr -d ' ')\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `bash "` + script + `"`
	settings := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settings, mustMarshal(t, map[string]any{"statusLine": map[string]any{"type": "command", "command": original}}), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := fixtureBytes(t, "statusline.json")
	before := shOut(t, original, payload)

	o := hooks.Options{TogoHome: filepath.Join(root, "togo"), Binary: "/opt/togo/bin/togo", ClaudeDir: claudeDir}
	for _, r := range hooks.Install(o, []string{hooks.Claude}) {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	var doc struct {
		StatusLine struct{ Command string } `json:"statusLine"`
	}
	b, _ := os.ReadFile(settings)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	// The installed line is guarded ([ -x BIN ] && exec BIN … || exec sh -c ORIG);
	// the words exec runs are the part between the guard's test and its fallback.
	inner := doc.StatusLine.Command
	if i := strings.Index(inner, " ] && exec "); i >= 0 {
		inner = inner[i+len(" ] && exec "):]
	}
	if i := strings.LastIndex(inner, " || exec sh -c "); i >= 0 {
		inner = inner[:i]
	}
	words := shWords(t, inner)
	if len(words) != 6 || words[0] != "/opt/togo/bin/togo" || words[5] != original {
		t.Fatalf("chained command %q -> %q", doc.StatusLine.Command, words)
	}
	code, out, _ := runWith(t, chainEnv(home), payload, words[1:]...)
	if code != 0 || out != before {
		t.Fatalf("code %d, chained output %q, original %q", code, out, before)
	}
	if calls := waitCalls(d); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"hook":"statusline"`) {
		t.Fatalf("Togo did not receive the status line: %+v", calls)
	}

	hooks.Uninstall(o, []string{hooks.Claude})
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), mustQuote(t, original)) || strings.Contains(string(b), "togo") {
		t.Fatalf("uninstall left %s", b)
	}
}

func waitCalls(d *fakeDaemon) []fakeCall {
	for range 200 {
		if c := d.recorded(); len(c) > 0 {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	return d.recorded()
}

func shOut(t *testing.T, cmd string, stdin []byte) string {
	t.Helper()
	c := exec.Command("sh", "-c", cmd)
	c.Stdin = bytes.NewReader(stdin)
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// shWords splits a command line the way sh does.
func shWords(t *testing.T, cmd string) []string {
	t.Helper()
	out, err := exec.Command("sh", "-c", "set -- "+cmd+`; for a; do printf '%s\0' "$a"; done`).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustQuote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
