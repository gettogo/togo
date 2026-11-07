package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakecmd", []byte(`
adapter: fakecmd
name: Fake commands
revision: 1
match: { agents: [fakecmd] }
capabilities: [commands, mode, model, effort]
sources:
  models: { static: [{ id: a, label: A }, { id: b, label: B, badge: 1M }], stability: documented }
screens:
  model: { primitive: picker, title: Model, capability: model, source: models, map: { item.id: .id, item.title: .label, item.badge: .badge }, on_select: set_model }
  effort:
    primitive: settings
    title: Effort
    capability: effort
    fields: [{ id: effort, label: Effort, kind: choice, value: '"high"', options: [low, high], action: set_effort }]
actions:
  set_model: { relaunch: { argv: [fakecmd, -m, '{{.id}}'] }, stability: documented }
  set_effort: { confirm: 'Change the effort?', cli: { argv: [rec, effort, '{{.value}}'] }, stability: documented }
  set_mode: { cli: { argv: [rec, mode, '{{.id}}'] }, stability: documented }
  undo: { role: destructive, confirm: 'Undo?', cli: { argv: [rec, undo] }, stability: documented }
  command: { cli: { argv: [rec, command, '{{.command}}', '{{.arguments}}'] }, stability: documented }
  send: { cli: { argv: [rec, send, '{{.text}}'] }, stability: documented }
modes: { choices: [{ id: ask, label: Ask }, { id: auto, label: Auto }], set: set_mode }
commands:
  static:
    - { name: model, screen: model }
    - { name: effort, screen: effort }
    - { name: mode, kind: choice, action: set_mode }
    - { name: undo, action: undo }
    - { name: add, hint: '<file>' }
    - { name: stats, opens: status }
`))
}

// cmdEngine serves one fakecmd session whose actions append their argv to
// rec.log in the session folder.
func cmdEngine(t *testing.T, id string) (*Engine, string) {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	rec := "#!/bin/sh\necho \"$@\" >> rec.log\n"
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte(rec), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakecmd", Managed: true, Cwd: home, Command: []string{"fakecmd"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e, home
}

// The / menu says how each command runs, with a choice's values in place.
func TestCompleteCommandKinds(t *testing.T) {
	const id = "s_fakecmd001"
	e, _ := cmdEngine(t, id)
	res, err := e.Complete(context.Background(), harness.CompleteRequest{Session: id, Kind: harness.CompleteCommand})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]harness.Completion{}
	for _, c := range res.Items {
		by[c.Title] = c
	}
	model := by["/model"]
	if model.Kind != spec.CommandChoice || model.Action != "set_model" || len(model.Choices) != 2 || model.Choices[1].Label != "B 1M" {
		t.Errorf("/model %+v", model)
	}
	effort := by["/effort"]
	if effort.Kind != spec.CommandChoice || effort.Action != "set_effort" || len(effort.Choices) != 2 || !effort.Choices[1].Selected || effort.Choices[0].Selected {
		t.Errorf("/effort %+v", effort)
	}
	mode := by["/mode"]
	if mode.Kind != spec.CommandChoice || mode.Action != "set_mode" || len(mode.Choices) != 2 || mode.Choices[0].ID != "ask" {
		t.Errorf("/mode %+v", mode)
	}
	if u := by["/undo"]; u.Kind != spec.CommandRun || u.Action != "undo" || u.Role != spec.RoleDestructive {
		t.Errorf("/undo %+v", u)
	}
	if a := by["/add"]; a.Kind != spec.CommandArgs || a.Hint != "<file>" {
		t.Errorf("/add %+v", a)
	}
	if s := by["/stats"]; s.Kind != spec.CommandRun || s.Opens != spec.OpensStatus {
		t.Errorf("/stats %+v", s)
	}
}

// A picker whose pick restarts the agent says so; a settings field whose
// action asks first carries the question.
func TestScreenSelectConfirm(t *testing.T) {
	const id = "s_fakecmd002"
	e, _ := cmdEngine(t, id)
	ctx := context.Background()
	m, err := e.Screen(ctx, id, "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := m.Picker.OnSelectConfirm
	if c == nil || c.Effect == nil || *c.Effect != harness.EffectRelaunch || c.Text != "Restart Fake commands?" {
		t.Fatalf("model confirm %+v", c)
	}
	s, err := e.Screen(ctx, id, "effort", nil)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Settings.Sections[0].Fields[0]
	if f.ActionConfirm == nil || f.ActionConfirm.Text != "Change the effort?" || f.ActionConfirm.Effect != nil {
		t.Fatalf("effort confirm %+v", f.ActionConfirm)
	}
}

// A / line on an adapter with a command action runs the harness's command
// (OpenCode's POST /session/{id}/command), not a prompt; other text is a
// prompt.
func TestSendSlashLineRunsTheCommand(t *testing.T) {
	const id = "s_fakecmd003"
	e, home := cmdEngine(t, id)
	ctx := context.Background()
	for _, text := range []string{"/init now please", "/review", "fix the bug", "/usr/bin is a path"} {
		r, err := e.Send(ctx, harness.SendRequest{Session: id, Text: text, Mode: harness.SendNow})
		if err != nil || r.Item.User == nil || r.Item.User.Text != text {
			t.Fatalf("send %q: %+v %v", text, r, err)
		}
	}
	var got []byte
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		got, _ = os.ReadFile(filepath.Join(home, "rec.log"))
		if strings.Count(string(got), "\n") >= 4 {
			break
		}
	}
	want := "command init now please\ncommand review \nsend fix the bug\nsend /usr/bin is a path\n"
	if string(got) != want {
		t.Fatalf("calls\n%s\nwant\n%s", got, want)
	}
}

func TestSlashCommand(t *testing.T) {
	for in, want := range map[string]string{"/init": "init|", " /review  pr 12 ": "review|pr 12", "/": "", "hello": "", "/a/b": "", "/x\ny": ""} {
		name, args, ok := slashCommand(in)
		got := ""
		if ok {
			got = name + "|" + args
		}
		if got != want {
			t.Errorf("slashCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
