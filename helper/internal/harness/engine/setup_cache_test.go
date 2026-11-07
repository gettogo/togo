package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakesignin lists providers through a CLI that counts its runs, cached
// for a minute as omp's are.
func init() {
	harness.RegisterAdapterFile("fakesignin", []byte(`
adapter: fakesignin
name: Fake sign-in
revision: 1
match: { agents: [fakesignin] }
capabilities: [model]
sources:
  models: { cli: { argv: [count, models] }, cache: 1m, stability: documented }
  providers: { cli: { argv: [count, providers] }, cache: 1m, stability: internal }
screens:
  model:
    primitive: picker
    title: Model
    capability: model
    source: models
    map: { item.id: .id, item.group: .p }
    on_select: use
    setup: { source: providers, map: { item.id: .id, item.title: .name }, action: login }
actions:
  use: { type: { text: '/use {{.id}}', submit: true }, stability: documented-command }
  login: { label: Sign in, type: { text: '/login {{.id}}', submit: true }, stability: documented-command }
`))
}

// Seen on the phone: after signing in, the picker kept "Not set up" and no
// new models until it was closed and opened a minute later, because the
// sign-in page's checks were answered from the one-minute cache. While a
// sign-in can still finish, screens read the sources again.
func TestScreensReadAfreshWhileSigningIn(t *testing.T) {
	const id = "s_fakesignin1"
	bin, home := t.TempDir(), t.TempDir()
	runs := filepath.Join(home, "runs.log")
	script := "#!/bin/sh\necho \"$1\" >> " + runs + "\ncase \"$1\" in models) echo '[{\"id\":\"a/1\",\"p\":\"a\"}]';; *) echo '[{\"id\":\"b\",\"name\":\"B\"}]';; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "count"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := func() time.Time { return now }
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakesignin", Managed: true, Cwd: home, Command: []string{"fakesignin"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, Now: clock,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "models")
	}
	read := func() {
		if _, err := e.Screen(context.Background(), id, "model", nil); err != nil {
			t.Fatal(err)
		}
	}
	read()
	read()
	if n := count(); n != 1 {
		t.Fatalf("models read %d times before a sign-in; want the cache", n)
	}
	if _, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "login", Target: "b",
		Args: map[string]json.RawMessage{"id": json.RawMessage(`"b"`)}}); err != nil {
		t.Fatal(err)
	}
	read()
	now = now.Add(3 * time.Second)
	read()
	if n := count(); n != 3 {
		t.Fatalf("models read %d times during the sign-in; want each check to read again", n)
	}
	now = now.Add(settingUpWindow)
	read()
	now = now.Add(3 * time.Second)
	read()
	if n := count(); n != 4 {
		t.Fatalf("models read %d times after the window; want the cache back", n)
	}
}
