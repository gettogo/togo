package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// A scripted harness: its adapter file, a mapper that replays records
// verbatim, and fakes for the pane.

const fakeAdapter = `
adapter: fake
name: Fake
revision: 1
match: { agents: [fake] }
harness: { version: [fakeharness, --version], pattern: '(\d+\.\d+\.\d+)', range: ">=1.0 <2" }
capabilities: [chat, chat.history, send.queue, interrupt, commands, mode, model, plan, status, needs.approvals]
images: { insert: per_path }
feeds:
  events:
    hook: { events: [Event] }
    mapper: fake.events
    stability: documented
sources:
  models:
    cli: { argv: [fakeharness, models] }
    select: '[.models[] | select(.hidden | not)]'
    cache: 1m
    stability: documented
  broken:
    cli: { argv: [fakeharness, fail] }
    stability: documented
screens:
  model:
    primitive: picker
    title: Model
    section: composer
    capability: model
    source: models
    map: { item.id: .id, item.title: .name, selected: $state.model }
    on_select: set_model
  broken:
    primitive: picker
    title: Broken
    section: more
    source: broken
actions:
  set_model:
    type: { text: '/model {{.id}}', submit: true, when_idle: true }
    verify: { source: events, select: $state.model, equals: '{{.id}}', within: 1s }
    stability: documented-command
  set_mode:
    keys: { keys: [BTab], cycle: true, max: 3 }
    verify: { source: events, select: $state.mode, equals: '{{.id}}', within: 1s }
    stability: documented
  interrupt:
    keys: { keys: [Escape] }
    stability: documented
modes:
  choices: [{ id: a, label: A }, { id: b, label: B }, { id: c, label: C }]
  current: events
  select: $state.mode
  set: set_mode
commands:
  static:
    - { name: model, hint: '[model]', detail: Set the model, screen: model }
    - { name: clear, detail: Start over }
`

func init() {
	harness.RegisterAdapterFile("fake", []byte(fakeAdapter))
	harness.RegisterMapper("fake.events", func(harness.Env) harness.Mapper { return &replay{state: map[string]string{}} })
}

// replay maps {"items": [...], "ops": [...], "state": {...}} records.
type replay struct {
	mu    sync.Mutex
	state map[string]string
}

func (r *replay) Name() string { return "fake.events" }

func (r *replay) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var in struct {
		Items []harness.Item    `json:"items"`
		Ops   []harness.Op      `json:"ops"`
		State map[string]string `json:"state"`
		Panic bool              `json:"panic"`
	}
	_ = json.Unmarshal(rec.Data, &in)
	if in.Panic {
		panic("scripted")
	}
	r.mu.Lock()
	for k, v := range in.State {
		r.state[k] = v
	}
	r.mu.Unlock()
	return in.Items, in.Ops
}

func (r *replay) State() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for k, v := range r.state {
		out[k] = v
	}
	return out
}

// ReadScreen reads "mode: x" from the fake pane.
func (r *replay) ReadScreen(text string) map[string]string {
	if m, ok := strings.CutPrefix(text, "mode: "); ok && m != "" {
		return map[string]string{"mode": m}
	}
	return nil
}

type fakeSessions map[string]session.Session

func (f fakeSessions) Resolve(_ context.Context, id string) (session.Session, error) {
	if s, ok := f[id]; ok {
		return s, nil
	}
	return session.Session{}, session.ErrNotFound
}

// fakePane types into a scripted harness: /model x sets the model, BTab
// moves the mode along a, b, c unless stuck.
type fakePane struct {
	mu    sync.Mutex
	typed []string
	keys  []string
	mode  string
	stuck bool
	bus   *source.Bus
}

func (p *fakePane) Type(_ context.Context, id, text string, _, _ bool) error {
	p.mu.Lock()
	p.typed = append(p.typed, text)
	p.mu.Unlock()
	if m, ok := strings.CutPrefix(text, "/model "); ok {
		deliver(id, map[string]any{"state": map[string]string{"model": m}})
	}
	return nil
}

func (p *fakePane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, keys...)
	if keys[0] == "BTab" && !p.stuck {
		p.mode = map[string]string{"a": "b", "b": "c", "c": "a"}[p.mode]
	}
	return nil
}

func (p *fakePane) Screen(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return "mode: " + p.mode, nil
}

type fakeMarker struct{ n int64 }

func (m *fakeMarker) Mark(context.Context, string) (harness.Anchor, error) {
	m.n++
	return harness.Anchor{Epoch: "e", Line: m.n}, nil
}

func (m *fakeMarker) Find(_ context.Context, _ string, a harness.Anchor) (harness.Location, error) {
	return harness.Location{Found: true, LinesFromBottom: 10 - a.Line}, nil
}

func deliver(id string, v any) {
	b, _ := json.Marshal(v)
	source.Hooks.Deliver(id, source.Payload{Kind: source.BusHook, Event: "Event", Data: b})
}

func fakeBin(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'fake " + version + "';;\n" +
		"models) echo '{\"models\":[{\"id\":\"x\",\"name\":\"X\"},{\"id\":\"y\",\"name\":\"Y\"},{\"id\":\"z\",\"hidden\":true}]}';;\n" +
		"*) exit 2;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "fakeharness"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newFake(t *testing.T, id, version string) (*Engine, *fakePane) {
	t.Helper()
	bin := fakeBin(t, version)
	home := t.TempDir()
	pane := &fakePane{mode: "a"}
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fake", Managed: true, Cwd: home},
			"s_shellshell": {ID: "s_shellshell", Agent: "shell", Managed: true, Cwd: home}},
		Typist: pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.2,
	})
	t.Cleanup(func() { e.Forget(id) })
	return e, pane
}

func TestEndToEndScriptedHarness(t *testing.T) {
	const id = "s_fakefake01"
	e, pane := newFake(t, id, "1.2.3")
	ctx := context.Background()

	d, err := e.Describe(ctx, id)
	if err != nil || d.Adapter == nil || d.Adapter.HarnessVersion != "1.2.3" || len(d.Screens) != 2 || d.Screens[0].ID != "model" || len(d.Degraded) != 0 {
		t.Fatalf("describe = %+v %v", d, err)
	}
	shell, err := e.Describe(ctx, "s_shellshell")
	if err != nil || shell.Adapter != nil || len(shell.Capabilities) != 0 {
		t.Fatalf("shell describe = %+v %v", shell, err)
	}
	if _, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, "s_shellshell", 0, 10); !errors.Is(err, harness.ErrNoAdapter) {
		t.Fatalf("shell open = %v", err)
	}

	deliver(id, map[string]any{"state": map[string]string{"mode": "a", "model": "x"},
		"items": []harness.Item{{ID: "u1", TurnID: "u1", Kind: harness.KindUser, User: &harness.UserBody{Text: "key sk-abcdefghijklmnopqrst"}}},
		"ops":   []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: &harness.Row{ID: "1", Title: "Step", State: harness.RowActive}}}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "u1"); return ok })

	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	res, err := e.Open(ctx, sub, id, 0, 50)
	if err != nil || len(res.Items) != 1 || res.Epoch != "e1" || res.Mission.Summary.Plan.Total != 1 {
		t.Fatalf("open = %+v %v", res, err)
	}
	u := res.Items[0]
	if u.User.Text != "key [redacted]" || u.User.Source != "unknown" || u.Anchor == nil || u.Anchor.Line != 1 {
		t.Fatalf("user item = %+v %+v", u.User, u.Anchor)
	}
	e.Release(sub, id)
	deliver(id, map[string]any{"panic": true})
	deliver(id, map[string]any{"items": []harness.Item{{ID: "u1", Kind: harness.KindUser, User: &harness.UserBody{Text: "edited"}}}})
	waitFor(t, func() bool { return len(sub.notes()) >= 1 })
	if n := sub.notes()[0]; n.Kind != harness.NoteItems || n.Items[0].Seq != 2 || n.Items[0].Anchor.Line != 1 {
		t.Fatalf("update = %+v", n)
	}

	sc, err := e.Screen(ctx, id, "model", nil)
	if err != nil || len(sc.Picker.Items) != 2 || sc.Picker.Selected != "x" || sc.Picker.OnSelect != "set_model" {
		t.Fatalf("screen = %+v %v", sc.Picker, err)
	}
	var fb *harness.FallbackError
	if _, err := e.Screen(ctx, id, "broken", nil); !errors.As(err, &fb) || fb.Reason != "source" {
		t.Fatalf("broken screen = %v", err)
	}
	if _, err := e.Screen(ctx, id, "nope", nil); !errors.Is(err, harness.ErrUnknownScreen) {
		t.Fatal(err)
	}

	act, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"y"`)}})
	if err != nil || act.Result != harness.ResultApplied {
		t.Fatalf("set_model = %+v %v", act, err)
	}
	act, _ = e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_mode", Args: map[string]json.RawMessage{"id": json.RawMessage(`"c"`)}})
	if act.Result != harness.ResultApplied || pane.mode != "c" || len(pane.keys) != 2 {
		t.Fatalf("set_mode = %+v mode %s keys %v", act, pane.mode, pane.keys)
	}
	if _, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_mode", Args: map[string]json.RawMessage{"id": json.RawMessage(`"q"`)}}); err == nil || len(pane.keys) != 2 {
		t.Fatalf("unknown mode = %v keys %v", err, pane.keys)
	}
	pane.stuck = true
	act, _ = e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_mode", Args: map[string]json.RawMessage{"id": json.RawMessage(`"b"`)}})
	if act.Result != harness.ResultFailed || !strings.Contains(act.Message, "did not change") || len(pane.keys) != 3 {
		t.Fatalf("stuck mode = %+v keys %v", act, pane.keys)
	}
	if _, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "nope"}); !errors.Is(err, harness.ErrUnknownAction) {
		t.Fatal(err)
	}

	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { s, _ := e.Summary(id); return s.Working })
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "now", Mode: harness.SendNow}); !errors.Is(err, harness.ErrBusy) {
		t.Fatalf("send now while working = %v", err)
	}
	sent, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "later", Mode: harness.SendQueue})
	if err != nil || !sent.Item.User.Queued || sent.Item.User.Source != "phone" {
		t.Fatalf("queued = %+v %v", sent, err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { it, _ := e.log.Get(id, sent.Item.ID); return !it.User.Queued })
	if pane.typed[len(pane.typed)-1] != "later" {
		t.Fatalf("typed = %v", pane.typed)
	}
	deliver(id, map[string]any{"items": []harness.Item{{ID: "u9", TurnID: "u9", Kind: harness.KindUser, User: &harness.UserBody{Text: "later"}}}})
	waitFor(t, func() bool { it, _ := e.log.Get(id, sent.Item.ID); return it.TurnID == "u9" })

	ir, err := e.Interrupt(ctx, id)
	if err != nil || ir.Result != harness.ResultTyped || pane.keys[len(pane.keys)-1] != "Escape" {
		t.Fatalf("interrupt = %+v %v", ir, err)
	}
	comp, err := e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteCommand, Query: "mo"})
	if err != nil || len(comp.Items) != 1 || comp.Items[0].Screen != "model" || comp.Items[0].Insert != "/model " {
		t.Fatalf("complete = %+v %v", comp, err)
	}
	page, err := e.History(ctx, id, 100, 10)
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("history = %+v %v", page, err)
	}
	loc, err := e.Locate(ctx, id, harness.Anchor{Line: 4})
	if err != nil || !loc.Found || loc.LinesFromBottom != 6 {
		t.Fatalf("locate = %+v %v", loc, err)
	}
	if sum, ok := e.Summary(id); !ok || sum.Plan == nil || sum.Working {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestVersionGateDegradesToTerminal(t *testing.T) {
	const id = "s_fakeold001"
	e, _ := newFake(t, id, "0.5.0")
	ctx := context.Background()
	d, err := e.Describe(ctx, id)
	if err != nil || d.Adapter == nil || len(d.Degraded) != len(d.Capabilities) || len(d.Screens) != 0 {
		t.Fatalf("describe = %+v %v", d, err)
	}
	var fb *harness.FallbackError
	if _, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 10); !errors.As(err, &fb) || fb.Reason != "version" {
		t.Fatalf("open = %v", err)
	}
	if _, ok := e.Summary(id); ok {
		t.Fatal("a gated session has no summary")
	}
}

func TestOverridesAndBadAdapters(t *testing.T) {
	dir := t.TempDir()
	override := strings.Replace(fakeAdapter, "name: Fake", "name: Fake Override", 1)
	_ = os.WriteFile(filepath.Join(dir, "fake.yaml"), []byte(override), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("adapter: broken\nunknown: 1\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(strings.Replace(fakeAdapter, "mapper: fake.events", "mapper: no.such", 1)), 0o600)
	var logs []string
	got := loadAdapters(dir, func(f string, a ...any) { logs = append(logs, f) })
	if got["fake"].Name != "Fake Override" || got["broken"] != nil {
		t.Fatalf("adapters = %+v", got)
	}
	if len(logs) < 2 {
		t.Fatalf("refusals not logged: %v", logs)
	}
}
