package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakecfg", []byte(`
adapter: fakecfg
name: Fake
revision: 1
match: { agents: [fakecfg] }
capabilities: [chat, settings]
sources:
  settings:
    cli: { argv: [cfg, list] }
    stability: documented
actions:
  set_flag:
    cli: { argv: [cfg, set, '{{.value}}'] }
    verify: { source: settings, select: .flag.value, equals: '{{.value}}', within: 1s, required: true, otherwise: 'A project setting overrides it.' }
    stability: documented
`))
}

// Seen live on omp 18.20: a project's settings overrode the user's, the
// app's toggle ran "omp config set" on the user's file, and the action
// came back applied while the setting in effect never changed. A command
// that ran is applied only when what it was for can be read back.
func TestCLIActionIsAppliedOnlyWhenItTookEffect(t *testing.T) {
	const id = "s_fakecfg001"
	bin, home := t.TempDir(), t.TempDir()
	// "set" writes the user's value; "list" reports the value in effect,
	// which a file named override pins to false.
	script := `#!/bin/sh
case "$1" in
set) echo "$2" > user ;;
list) v=$(cat user 2>/dev/null || echo false); [ -e override ] && v=false; echo "{\"flag\":{\"value\":$v}}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "cfg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakecfg", Managed: true, Cwd: home, Command: []string{"fakecfg"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.2,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	set := func() harness.ActionResult {
		r, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_flag", Args: map[string]json.RawMessage{"value": json.RawMessage("true")}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := os.WriteFile(filepath.Join(home, "override"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := set(); r.Result != harness.ResultFailed || r.Message != "A project setting overrides it." {
		t.Fatalf("overridden setting = %+v, want failed with the reason", r)
	}
	os.Remove(filepath.Join(home, "override"))
	if r := set(); r.Result != harness.ResultApplied {
		t.Fatalf("setting that took effect = %+v, want applied", r)
	}
}
