package hooks

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// An install from an older togo (the eight first Claude events, three
// Codex events) reads as outdated; installing again adds the new events
// once, keeps the user's hooks, and a third run changes nothing.
func TestReinstallUpgradesOlderInstall(t *testing.T) {
	e := newEnv(t)
	hooks := map[string]any{"PreToolUse": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "lint.sh"}}}}}
	for _, ev := range claudeBase[:8] {
		g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand(e.o.Binary, Claude, ev)}}}
		list, _ := hooks[ev].([]any)
		hooks[ev] = append(list, g)
	}
	old, _ := json.MarshalIndent(map[string]any{"model": "opus", "hooks": hooks}, "", "  ")
	write(t, settingsPath(e), old)
	codexOld := map[string]any{}
	for _, ev := range []string{"UserPromptSubmit", "PermissionRequest", "Stop"} {
		codexOld[ev] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand(e.o.Binary, Codex, ev)}}}}
	}
	b, _ := json.Marshal(map[string]any{"hooks": codexOld})
	write(t, filepath.Join(e.o.CodexDir, "hooks.json"), b)

	st := Status(e.o)
	if st[Claude] != StatusOutdated || st[Codex] != StatusOutdated {
		t.Fatalf("status before %v", st)
	}
	rs := Install(e.o, Agents)
	mustOK(t, rs)
	for _, r := range rs {
		if r.State != "installed" {
			t.Errorf("%s upgrade state %s", r.Agent, r.State)
		}
	}
	if st := Status(e.o); st[Claude] != StatusInstalled || st[Codex] != StatusInstalled {
		t.Fatalf("status after %v", st)
	}
	got := read(t, settingsPath(e))
	for _, ev := range claudeBase {
		if n := strings.Count(got, "hook claude "+ev+`"`); n != 1 {
			t.Errorf("%s has %d togo entries", ev, n)
		}
	}
	if !strings.Contains(got, "lint.sh") || !strings.Contains(got, `"model": "opus"`) {
		t.Errorf("user settings lost: %s", got)
	}
	first, firstCodex := got, read(t, filepath.Join(e.o.CodexDir, "hooks.json"))
	for _, r := range Install(e.o, Agents) {
		if r.Err != nil || r.State != "unchanged" {
			t.Errorf("%s third run %s %v", r.Agent, r.State, r.Err)
		}
	}
	if read(t, settingsPath(e)) != first || read(t, filepath.Join(e.o.CodexDir, "hooks.json")) != firstCodex {
		t.Error("third run changed a file")
	}
}

func TestCoverage(t *testing.T) {
	ev := []string{"a", "b"}
	if coverage(map[string]bool{"a": true, "b": true}, ev) != StatusInstalled ||
		coverage(map[string]bool{"a": true}, ev) != StatusOutdated ||
		coverage(nil, ev) != StatusAbsent {
		t.Fatal("coverage")
	}
}
