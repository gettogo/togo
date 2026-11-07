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
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// seqPane is a pane whose mode moves along next on every BTab.
type seqPane struct {
	mu    sync.Mutex
	mode  string
	next  map[string]string
	keys  []string
	typed []string
}

func (p *seqPane) Type(_ context.Context, _, text string, _, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typed = append(p.typed, text)
	return nil
}

func (p *seqPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, keys...)
	if keys[0] == "BTab" {
		p.mode = p.next[p.mode]
	}
	return nil
}

func (p *seqPane) Screen(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return "mode: " + p.mode, nil
}

func (p *seqPane) snapshot() (keys, typed []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.keys...), append([]string(nil), p.typed...)
}

func newSeq(t *testing.T, id string, pane *seqPane, relaunch func(context.Context, string, []string) error) *Engine {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	home := t.TempDir()
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fake", Managed: true, Cwd: home, Command: []string{"fake"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home, Relaunch: relaunch,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e
}

func setMode(e *Engine, id, mode string) (harness.ActionResult, error) {
	return e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "set_mode",
		Args: map[string]json.RawMessage{"id": json.RawMessage(`"` + mode + `"`)}})
}

// A caller that names the chosen item only as the target still fills .id.
func TestTargetStandsInForID(t *testing.T) {
	const id = "s_typetgt01"
	pane := &seqPane{mode: "a"}
	e := newSeq(t, id, pane, nil)
	deliver(id, map[string]any{"state": map[string]string{"working": "false", "model": "x"}})
	waitFor(t, func() bool { s, _ := e.served(context.Background(), id); return e.idleNow(s) })
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "set_model", Target: "z"})
	if err != nil || res.Message != "Sent /model z" {
		t.Fatalf("target only = %+v %v", res, err)
	}
}

// A picker's choice comes back with the picker read again, so the phone
// shows the new selection or role holder without reopening it.
func TestPickerActionsReturnThePicker(t *testing.T) {
	const id = "s_typescr01"
	e := newSeq(t, id, &seqPane{mode: "a"}, nil)
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.screenFor(s, "set_model"); got != "model" {
		t.Fatalf("set_model rereads %q", got)
	}
	if got := e.screenFor(s, "interrupt"); got != "" {
		t.Fatalf("interrupt rereads %q", got)
	}
}

func TestCycleStopsOnUnknownMode(t *testing.T) {
	const id = "s_cycleunkn1"
	pane := &seqPane{mode: "a", next: map[string]string{"a": "z"}}
	e := newSeq(t, id, pane, nil)
	res, err := setMode(e, id, "c")
	keys, _ := pane.snapshot()
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "does not know") || len(keys) < 1 {
		t.Fatalf("res = %+v %v keys %v", res, err, keys)
	}
}

func TestCycleStopsWhenModesComeRound(t *testing.T) {
	const id = "s_cycleloop1"
	pane := &seqPane{mode: "a", next: map[string]string{"a": "b", "b": "a"}}
	e := newSeq(t, id, pane, nil)
	res, _ := setMode(e, id, "c")
	keys, _ := pane.snapshot()
	// Back where it started, so it says so and presses nothing more.
	if res.Result != harness.ResultFailed || !strings.Contains(res.Message, "back in A") || len(keys) != 2 {
		t.Fatalf("res = %+v keys %v", res, keys)
	}
}

func TestCycleReachesTargetAndNeverPressesWhenThere(t *testing.T) {
	const id = "s_cycleok001"
	pane := &seqPane{mode: "a", next: map[string]string{"a": "b", "b": "c", "c": "a"}}
	e := newSeq(t, id, pane, nil)
	if res, _ := setMode(e, id, "c"); res.Result != harness.ResultApplied {
		t.Fatalf("res = %+v", res)
	}
	if res, _ := setMode(e, id, "c"); res.Result != harness.ResultApplied {
		t.Fatalf("again = %+v", res)
	}
	if keys, _ := pane.snapshot(); len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
}

func TestKeysRefuseMenuDriving(t *testing.T) {
	const id = "s_keysrefus1"
	pane := &seqPane{mode: "a"}
	e := newSeq(t, id, pane, nil)
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range [][]string{{"Up"}, {"Down"}, {"Tab"}, {"BTab"}, {"Escape", "Left"}} {
		if err := e.sendKeys(context.Background(), s, false, k...); !errors.Is(err, errKeyRefused) {
			t.Fatalf("%v sent: %v", k, err)
		}
	}
	act := spec.Action{Routes: spec.Routes{Keys: &spec.Keys{Keys: []string{"BTab"}}}, Stability: "documented"}
	if res, _ := e.run(context.Background(), s, "x", act, map[string]any{}); res.Result != harness.ResultFailed {
		t.Fatalf("BTab without a cycle = %+v", res)
	}
	for _, text := range []string{"/model x\n/quit", "model x", "/", "/x\x1b[A", "/x\ty"} {
		typed := spec.Action{Routes: spec.Routes{Type: &spec.Type{Text: text, Submit: true}}, Stability: "documented"}
		if res, err := e.run(context.Background(), s, "x", typed, map[string]any{}); err == nil && res.Result != harness.ResultFailed {
			t.Fatalf("%q typed: %+v", text, res)
		}
	}
	if keys, typed := pane.snapshot(); len(keys)+len(typed) != 0 {
		t.Fatalf("pane got keys %v typed %v", keys, typed)
	}
}

func TestTypedCommandWaitsForIdle(t *testing.T) {
	const id = "s_typeidle01"
	pane := &seqPane{mode: "a"}
	e := newSeq(t, id, pane, nil)
	deliver(id, map[string]any{"state": map[string]string{"working": "true", "model": "x"}})
	waitFor(t, func() bool { s, _ := e.served(context.Background(), id); return !e.idleNow(s) })
	_, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"y"`)}})
	if !errors.Is(err, harness.ErrBusy) {
		t.Fatalf("typed while working: %v", err)
	}
	if _, typed := pane.snapshot(); len(typed) != 0 {
		t.Fatalf("typed %v while working", typed)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"y"`)}})
	if err != nil || res.Result != harness.ResultTyped || res.Message != "Sent /model y" {
		t.Fatalf("idle = %+v %v", res, err)
	}
}

func TestQueueDrainsInOrderOnePerTurn(t *testing.T) {
	const id = "s_queueord01"
	pane := &seqPane{mode: "a"}
	e := newSeq(t, id, pane, nil)
	ctx := context.Background()
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { s, _ := e.served(ctx, id); return !e.idleNow(s) })
	var ids []string
	for _, text := range []string{"first", "second", "third"} {
		r, err := e.Send(ctx, harness.SendRequest{Session: id, Text: text, Mode: harness.SendQueue})
		if err != nil || !r.Item.User.Queued {
			t.Fatalf("queue %s: %+v %v", text, r, err)
		}
		ids = append(ids, r.Item.ID)
	}
	if res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: harness.ActionStop, Target: ids[1]}); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("cancel = %+v %v", res, err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { _, typed := pane.snapshot(); return len(typed) == 1 })
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { s, _ := e.served(ctx, id); return !e.idleNow(s) })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { _, typed := pane.snapshot(); return len(typed) == 2 })
	if _, typed := pane.snapshot(); typed[0] != "first" || typed[1] != "third" {
		t.Fatalf("typed %v", typed)
	}
}

func TestRelaunchKeepsTheProgram(t *testing.T) {
	const id = "s_relaunch01"
	var got []string
	e := newSeq(t, id, &seqPane{mode: "a"}, func(_ context.Context, _ string, argv []string) error { got = argv; return nil })
	s, _ := e.served(context.Background(), id)
	run := func(argv ...string) harness.ActionResult {
		act := spec.Action{Routes: spec.Routes{Relaunch: &spec.Relaunch{Argv: argv}}, Stability: "documented"}
		res, err := e.run(context.Background(), s, "x", act, map[string]any{"id": "m2"})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("fake", "-m", "{{.id}}"); res.Result != harness.ResultRelaunched || strings.Join(got, " ") != "fake -m m2" {
		t.Fatalf("relaunch = %+v %v", res, got)
	}
	for _, argv := range [][]string{{"sh", "-c", "x"}, {"/tmp/fake"}, {"{{.missing}}"}} {
		if res := run(argv...); res.Result != harness.ResultFailed {
			t.Fatalf("%v relaunched: %+v", argv, res)
		}
	}
}

func TestWriteIsAtomicWithBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(p, []byte(`{"model":{"name":"a"},"keep":1}`), 0o640)
	if err := setJSONKey(p, "model.name", "b"); err != nil {
		t.Fatal(err)
	}
	if err := setJSONKey(p, "general.mode", "plan"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(p)
	if json.Unmarshal(b, &doc) != nil || doc["keep"] != 1.0 || doc["model"].(map[string]any)["name"] != "b" {
		t.Fatalf("doc = %s", b)
	}
	if bak, _ := os.ReadFile(p + backupSuffix); string(bak) != `{"model":{"name":"a"},"keep":1}` {
		t.Fatalf("backup = %s", bak)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", st.Mode())
	}
	if err := setJSONKey(p, "keep.x", "1"); err == nil {
		t.Fatal("key through a value was written")
	}
	for _, k := range []string{"", "a..b", ".a"} {
		if err := setJSONKey(p, k, "1"); err == nil {
			t.Fatalf("key %q written", k)
		}
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`{not json`), 0o600)
	if err := setJSONKey(bad, "a", "1"); err == nil {
		t.Fatal("unreadable file replaced")
	}
	if b, _ := os.ReadFile(bad); string(b) != `{not json` {
		t.Fatal("unreadable file changed")
	}
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.Contains(en.Name(), ".togo-") && !strings.HasSuffix(en.Name(), backupSuffix) {
			t.Fatalf("temporary file left: %s", en.Name())
		}
	}
}

func TestWriteConfinedOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	_ = os.Symlink(filepath.Join(outside, "x.json"), filepath.Join(home, "link.json"))
	env := harness.Env{Cwd: home, Vars: map[string]string{"home": home}}
	for _, p := range []string{"~/link.json", filepath.Join(outside, "y.json"), "~/../escape.json"} {
		if _, err := confineWrite(p, env); err == nil {
			t.Fatalf("%s allowed", p)
		}
	}
	if _, err := confineWrite("~/ok.json", env); err != nil {
		t.Fatal(err)
	}
}
