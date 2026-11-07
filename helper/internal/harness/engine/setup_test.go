package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakesetup", []byte(`
adapter: fakesetup
name: Fake setup
revision: 1
match: { agents: [fakesetup] }
capabilities: [model]
sources:
  models: { static: [{ id: a/1, p: a }, { id: b/1, p: b }], stability: documented }
  providers: { static: [{ id: a, name: A }, { id: c, name: Cee }, { id: c, name: Cee again }, { name: no id }], stability: internal }
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

// The "Not set up" section lists the providers without models, once each,
// with the action that signs in.
func TestPickerSetupSection(t *testing.T) {
	const id = "s_fakesetup01"
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakesetup", Managed: true, Cwd: t.TempDir(), Command: []string{"fakesetup"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: t.TempDir(),
		Log: &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	sc, err := e.Screen(context.Background(), id, "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	p := sc.Picker
	if len(p.Items) != 2 {
		t.Fatalf("items = %+v", p.Items)
	}
	if len(p.Setup) != 1 || p.Setup[0].ID != "c" || p.Setup[0].Title != "Cee" {
		t.Fatalf("setup = %+v", p.Setup)
	}
	if p.SetupAction == nil || p.SetupAction.ID != "login" || p.SetupAction.Label != "Sign in" {
		t.Fatalf("setup action = %+v", p.SetupAction)
	}
}

// Only a picker's setup action drops the session's cached sources.
func TestSetsUp(t *testing.T) {
	screens := map[string]spec.Screen{"model": {Setup: &spec.PickerSetup{Action: "login"}}, "other": {}}
	if !setsUp(screens, "login") || setsUp(screens, "use") {
		t.Fatal("setsUp")
	}
}
