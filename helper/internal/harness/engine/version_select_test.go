package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// Two adapters serve one agent, one per major version (OpenCode 1.x and
// 2.x): the installed version picks.
func init() {
	for _, f := range []string{`
adapter: vsel1
name: One
revision: 1
match: { agents: [vsel] }
harness: { version: [fakeharness, --version], pattern: '(\d+\.\d+\.\d+)', range: ">=1 <2" }
capabilities: [chat]
`, `
adapter: vsel2
name: Two
revision: 1
match: { agents: [vsel] }
harness: { version: [fakeharness, --version], pattern: '(\d+\.\d+\.\d+)', range: ">=2 <3" }
capabilities: [chat]
`} {
		harness.RegisterAdapterFile(map[bool]string{true: "vsel1", false: "vsel2"}[f[10:15] == "vsel1"], []byte(f))
	}
}

func TestAdapterPickedByVersion(t *testing.T) {
	for version, want := range map[string]string{"1.18.20": "vsel1", "2.2.5": "vsel2", "3.0.0": "vsel1"} {
		bin := fakeBin(t, version)
		id := "s_vsel" + map[string]string{"vsel1": "aaaa", "vsel2": "bbbb"}[want] + version[:1]
		e := New(Config{
			Sessions:  fakeSessions{id: session.Session{ID: id, Agent: "vsel", Managed: true, Cwd: t.TempDir(), Command: []string{"vsel"}}},
			Home:      t.TempDir(),
			LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
			Log:       &Log{NewEpoch: func() string { return "e1" }},
		})
		d, err := e.Describe(context.Background(), id)
		e.Forget(id)
		if err != nil {
			t.Fatal(err)
		}
		if d.Adapter == nil || d.Adapter.ID != want {
			t.Errorf("version %s: adapter %+v, want %s", version, d.Adapter, want)
		}
	}
}
