package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakeflaky", []byte(`
adapter: fakeflaky
name: Fake flaky version
revision: 1
match: { agents: [fakeflaky] }
harness: { version: [flaky, --version], pattern: '^(\d+\.\d+\.\d+)', range: ">=1 <2" }
capabilities: [interrupt]
actions:
  interrupt: { keys: { keys: [Escape] }, stability: documented }
`))
}

// A version command that fails to run once (a loaded machine, a node
// script before the login PATH is read) is asked again: the session gets
// its adapter instead of the terminal for its whole life.
func TestGateRetriesAFailedVersionCommand(t *testing.T) {
	old := gateRetries
	gateRetries = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { gateRetries = old })
	const id = "s_flaky00001"
	bin, home := t.TempDir(), t.TempDir()
	mark := filepath.Join(home, "ran")
	script := "#!/bin/sh\nif [ ! -e " + mark + " ]; then : > " + mark + "; exit 1; fi\necho 1.2.3\n"
	if err := os.WriteFile(filepath.Join(bin, "flaky"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeflaky", Managed: true, Cwd: home, Command: []string{"flaky"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
	})
	t.Cleanup(func() { e.Forget(id) })
	d, err := e.Describe(context.Background(), id)
	if err != nil || d.Adapter == nil || d.Adapter.HarnessVersion != "1.2.3" || len(d.Degraded) != 0 {
		t.Fatalf("describe %+v %v degraded %v", d.Adapter, err, d.Degraded)
	}
}

// A failure to run is not cached: the next session asks again.
func TestGateDoesNotCacheARunFailure(t *testing.T) {
	bin, home := t.TempDir(), t.TempDir()
	mark := filepath.Join(home, "ran")
	script := "#!/bin/sh\nif [ ! -e " + mark + " ]; then : > " + mark + "; exit 1; fi\necho 1.2.3\n"
	if err := os.WriteFile(filepath.Join(bin, "flaky"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &adapter{Adapter: &spec.Adapter{}}
	a.Harness.Version = []string{"flaky", "--version"}
	a.Harness.Pattern, a.Harness.Range = `^(\d+\.\d+\.\d+)`, ">=1 <2"
	env := harness.Env{Cwd: home, Vars: map[string]string{"path": bin + ":/bin:/usr/bin", "home": home}}
	var g gate
	if r := g.check(context.Background(), a, env); r.ok || !r.transient {
		t.Fatalf("first %+v", r)
	}
	if r := g.check(context.Background(), a, env); !r.ok || r.version != "1.2.3" {
		t.Fatalf("second %+v", r)
	}
}
