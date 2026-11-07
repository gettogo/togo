package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakeempty", []byte(`
adapter: fakeempty
name: Fake empty
revision: 1
match: { agents: [fakeempty] }
capabilities: [status, resume, plan, compact]
sources:
  sessions: { file: { glob: '~/.fakeempty/sessions/*/events.jsonl', format: jsonl }, stability: internal }
  todos: { file: { path: '~/.fakeempty/todos/{{.state.harness_session}}.json' }, select: .todos, stability: internal }
  missing: { file: { path: '~/.fakeempty/never.json' }, stability: internal }
screens:
  usage: { primitive: status, title: Usage, section: status, capability: status }
  resume: { primitive: picker, title: Resume, capability: resume, source: sessions, on_select: resume }
  plan: { primitive: checklist, title: Plan, capability: plan, source: todos }
  never: { primitive: picker, title: Never, source: missing, on_select: resume }
actions:
  resume: { relaunch: { argv: [fakeempty, --resume, '{{.id}}'] }, stability: documented }
  compact: { confirm: 'Compact now?', type: { text: '/compact' }, stability: documented-command }
status:
  actions: [compact]
`))
}

// No screen is a dead end before the harness wrote anything: an empty
// glob, a missing file, a session id not known yet and a status block not
// reported yet all read as empty screens.
func TestEmptyStatesAreScreens(t *testing.T) {
	const id = "s_fakeempty1"
	home := t.TempDir()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeempty", Managed: true, Cwd: home, Command: []string{"fakeempty"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"usage", "resume", "plan", "never"} {
		sc, err := e.Screen(ctx, id, name, nil)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		switch {
		case sc.Status != nil:
			if len(sc.Status.Actions) != 1 || sc.Status.Actions[0].ID != "compact" || sc.Status.Actions[0].Confirm != "Compact now?" {
				t.Errorf("status actions %+v", sc.Status.Actions)
			}
		case sc.Picker != nil:
			if len(sc.Picker.Items) != 0 {
				t.Errorf("%s items %+v", name, sc.Picker.Items)
			}
		case sc.Checklist != nil:
			if len(sc.Checklist.Items) != 0 {
				t.Errorf("%s items %+v", name, sc.Checklist.Items)
			}
		default:
			t.Errorf("%s: %+v", name, sc)
		}
	}
}

func TestNotYet(t *testing.T) {
	raw := []byte(`{"http":{"path":"/session/{{.state.harness_session}}/todo"}}`)
	if !notYet(raw, map[string]string{}) || notYet(raw, map[string]string{"harness_session": "ses_1"}) || notYet([]byte(`{"http":{"path":"/session"}}`), nil) {
		t.Fatal("notYet")
	}
}

func TestSlotsAndPart(t *testing.T) {
	for _, c := range []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"1": []any{"B"}, "4": "typed"}, `[[],["B"],[],[],["typed"]]`},
		{map[string]any{"0": []any{"a", "b"}}, `[["a","b"]]`},
		{map[string]any{}, `[]`},
	} {
		got, err := slotsFilter(c.in)
		if err != nil || got != c.want {
			t.Errorf("slots(%v) = %s %v, want %s", c.in, got, err, c.want)
		}
	}
	for _, bad := range []map[string]any{{"x": "a"}, {"16": "a"}, {"01": "a"}, {"0": []any{1}}} {
		if _, err := slotsFilter(bad); err == nil {
			t.Errorf("slots(%v) accepted", bad)
		}
	}
	if part(":", 2, "global:relay:browser") != "browser" || part(":", 1, "jobs") != "" || part(":", 0, "jobs") != "jobs" {
		t.Error("part")
	}
}
