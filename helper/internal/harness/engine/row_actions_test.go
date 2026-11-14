package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakerows", []byte(`
adapter: fakerows
name: Fake rows
revision: 1
match: { agents: [fakerows] }
capabilities: [mcp]
sources:
  servers:
    static:
      - { id: one, state: connected, actions: [turn_off] }
      - { id: two, state: disabled, actions: [turn_on, gone] }
      - { id: three, state: pending, actions: [] }
      - { id: four, state: failed }
    stability: documented
screens:
  servers: { primitive: liveList, title: Servers, capability: mcp, source: servers, map: { item.actions: .actions } }
  plain: { primitive: liveList, title: Plain, capability: mcp, source: servers }
actions:
  turn_on: { label: Connect, cli: { argv: [rec, 'on', '{{.target}}'] }, stability: documented }
  turn_off: { confirm: 'Turn it off?', cli: { argv: [rec, 'off', '{{.target}}'] }, stability: documented }
`))
}

// A live list had no way to offer an action on one of its rows (seen on
// OpenCode 2.3.0: the MCP list showed a connected server and nothing to do
// with it). A screen's item.actions names, per item, the actions its row
// offers, so a row offers only what fits its state.
func TestLiveListRowsOfferTheActionsTheirItemNames(t *testing.T) {
	const id = "s_fakerows001"
	home := t.TempDir()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakerows", Managed: true, Cwd: home, Command: []string{"fakerows"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	scr, err := e.Screen(ctx, id, "servers", nil)
	if err != nil || scr.LiveList == nil || len(scr.LiveList.Rows) != 4 {
		t.Fatalf("screen %+v %v", scr, err)
	}
	rows := scr.LiveList.Rows
	if a := rows[0].Actions; len(a) != 1 || a[0].ID != "turn_off" || a[0].Label != "Turn off" || a[0].Confirm != "Turn it off?" {
		t.Errorf("row one offers %+v", a)
	}
	// An action the adapter does not declare is not offered.
	if a := rows[1].Actions; len(a) != 1 || a[0].ID != "turn_on" || a[0].Label != "Connect" {
		t.Errorf("row two offers %+v", a)
	}
	for _, r := range rows[2:] {
		if len(r.Actions) != 0 {
			t.Errorf("row %s offers %+v", r.ID, r.Actions)
		}
	}
	// A screen that names no item.actions is as before.
	plain, err := e.Screen(ctx, id, "plain", nil)
	if err != nil || plain.LiveList == nil {
		t.Fatalf("screen %+v %v", plain, err)
	}
	for _, r := range plain.LiveList.Rows {
		if r.Actions != nil {
			t.Errorf("plain row %s offers %+v", r.ID, r.Actions)
		}
	}
}
