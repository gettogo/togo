package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// With the togo binary gone, a plain hook exits 0 and prints nothing, and
// a chained status line still prints the user's own line.
func TestGuardedCommandsSurviveAMissingBinary(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "it's gone", "togo")
	out, err := exec.Command("sh", "-c", hookCommand(gone, Claude, "Stop")).CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Errorf("plain hook: %v %q", err, out)
	}
	out, err = exec.Command("sh", "-c", chainCommand(gone, "echo 'my line'")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "my line" {
		t.Errorf("chain: %v %q", err, out)
	}
}

// With the binary present, the guard runs it with the same arguments.
func TestGuardedCommandsRunThePresentBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "togo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"ran $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c", hookCommand(bin, Claude, "Stop")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ran hook claude Stop" {
		t.Errorf("plain hook: %v %q", err, out)
	}
	out, err = exec.Command("sh", "-c", chainCommand(bin, "echo mine")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ran hook claude statusline --then echo mine" {
		t.Errorf("chain: %v %q", err, out)
	}
}

func TestGuardRecognition(t *testing.T) {
	g := hookCommand("/opt/togo/bin/togo", Claude, "Stop")
	if !plainCommand(g, Claude) || !owned(g, Claude) {
		t.Errorf("guarded form not recognised: %q", g)
	}
	if !plainCommand(bareHookCommand("/opt/togo/bin/togo", Claude, "Stop"), Claude) {
		t.Error("older bare form not recognised")
	}
	mixed := "[ -x '/opt/x/togo' ] && exec '/opt/togo/bin/togo' hook claude Stop || true"
	if plainCommand(mixed, Claude) {
		t.Error("a guard naming another binary counts as Togo's form")
	}
	if !chained(chainCommand("/b/togo", "x")) {
		t.Error("guarded chain not recognised")
	}
	// Harnesses that run hooks without a shell keep the bare form.
	if c := hookCommand("/b/togo", Codex, "Stop"); strings.HasPrefix(c, "[") {
		t.Errorf("codex hook guarded: %q", c)
	}
}

// An install from before the guard (bare chain) upgrades to the guarded
// chain.
func TestBareChainUpgradesToGuarded(t *testing.T) {
	e, _ := withUsersLine(t)
	bare := bareHookCommand(e.o.Binary, Claude, "statusline") + " --then " + shellQuote(usersLine)
	doc, _ := json.MarshalIndent(map[string]any{"statusLine": map[string]any{"type": "command", "command": bare}}, "", "  ")
	write(t, settingsPath(e), doc)
	if err := saveStatusLineState(e.o.TogoHome, statusLineState{Original: usersLine, Chained: bare}); err != nil {
		t.Fatal(err)
	}
	mustOK(t, Install(e.o, []string{Claude}))
	if got := statusLineCommand(t, e); got != chainCommand(e.o.Binary, usersLine) {
		t.Fatalf("command %q", got)
	}
}
