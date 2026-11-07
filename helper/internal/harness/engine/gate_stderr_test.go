package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakeerrver", []byte(`
adapter: fakeerrver
name: Fake stderr version
revision: 1
match: { agents: [fakeerrver] }
harness: { version: [errver, --version], pattern: '^(\d+\.\d+\.\d+)', range: ">=0.73 <0.80", stderr: merge }
capabilities: [interrupt]
actions:
  interrupt: { keys: { keys: [Escape] }, stability: documented }
`))
}

// A harness that prints its version on stderr (pi without a terminal)
// passes the gate when its file says stderr: merge.
func TestGateReadsStderrWhenAsked(t *testing.T) {
	const id = "s_errver0001"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "errver"), []byte("#!/bin/sh\necho 0.73.1 >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeerrver", Managed: true, Cwd: home, Command: []string{"errver"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
	})
	t.Cleanup(func() { e.Forget(id) })
	d, err := e.Describe(context.Background(), id)
	if err != nil || d.Adapter == nil || d.Adapter.HarnessVersion != "0.73.1" {
		t.Fatalf("describe %+v %v", d.Adapter, err)
	}
}
