package engine

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/wire"
)

// stopPane is a pane whose harness reacts to keys as scripted: react sees
// every key group sent and may end the turn or clear an approval.
type stopPane struct {
	mu    sync.Mutex
	keys  [][]string
	react func(keys []string, n int)
}

func (p *stopPane) Type(context.Context, string, string, bool, bool) error { return nil }

func (p *stopPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	p.keys = append(p.keys, append([]string(nil), keys...))
	n, react := len(p.keys), p.react
	p.mu.Unlock()
	if react != nil {
		react(keys, n)
	}
	return nil
}

func (p *stopPane) sent() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, k := range p.keys {
		out = append(out, strings.Join(k, "+"))
	}
	return strings.Join(out, " ")
}

// switchApprovals has one approval waiting until cleared.
type switchApprovals struct{ waiting atomic.Bool }

func (a *switchApprovals) Pending(string) []wire.Approval {
	if a.waiting.Load() {
		return []wire.Approval{{RequestID: "r_stop00001", Title: "Run a command?"}}
	}
	return nil
}

func (a *switchApprovals) Answer(context.Context, string, string, string) error { return nil }

func stopEngine(t *testing.T, id string, pane *stopPane, status *atomic.Value, approvals Approvals) (*Engine, *sess) {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	home := t.TempDir()
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fake", Managed: true, Cwd: home, Command: []string{"fake"}}},
		Typist:   pane, Marker: &fakeMarker{}, Home: home, Approvals: approvals,
		Status:    func(string) string { v, _ := status.Load().(string); return v },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

// working reports a turn through the fake mapper and waits for the engine
// to see it.
func working(t *testing.T, e *Engine, s *sess, w string) {
	t.Helper()
	deliver(s.id, map[string]any{"state": map[string]string{"working": w}})
	waitFor(t, func() bool { return e.idleNow(s) == (w == "false") })
}

func idleStatus() *atomic.Value {
	var v atomic.Value
	v.Store("idle")
	return &v
}

var claudeStop = &spec.Keys{Keys: []string{"Escape"}, Until: spec.UntilIdle, Max: 2,
	Then: []string{"C-x", "C-k", "C-x", "C-k"}}

// The owner's case: a background agent keeps the session working at an
// idle prompt, where Escape does nothing. Stop sends the chord that stops
// background agents after the first Escape, and presses Escape only once.
func TestStopEndsBackgroundAgents(t *testing.T) {
	const id = "s_stopbgag01"
	pane := &stopPane{}
	e, s := stopEngine(t, id, pane, idleStatus(), nil)
	working(t, e, s, "true")
	pane.react = func(keys []string, _ int) {
		if keys[0] == "C-x" {
			deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
		}
	}
	res, err := e.stopUntilIdle(context.Background(), s, claudeStop)
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("stop = %+v %v", res, err)
	}
	if got := pane.sent(); got != "Escape C-x+C-k+C-x+C-k" {
		t.Fatalf("sent %q", got)
	}
}

// A press that does not end the turn is followed by another, verified
// after each, up to max.
func TestStopPressesUntilIdle(t *testing.T) {
	const id = "s_stopagain1"
	pane := &stopPane{}
	e, s := stopEngine(t, id, pane, idleStatus(), nil)
	working(t, e, s, "true")
	pane.react = func(_ []string, n int) {
		if n == 2 {
			deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
		}
	}
	k := &spec.Keys{Keys: []string{"Escape"}, Until: spec.UntilIdle, Max: 3}
	res, _ := e.stopUntilIdle(context.Background(), s, k)
	if res.Result != harness.ResultApplied || pane.sent() != "Escape Escape" {
		t.Fatalf("stop = %+v, sent %q", res, pane.sent())
	}
}

// Stop is bounded: a turn that never ends is reported, not pressed at
// forever, and Then goes once.
func TestStopGivesUpAfterMax(t *testing.T) {
	const id = "s_stopmax001"
	pane := &stopPane{}
	e, s := stopEngine(t, id, pane, idleStatus(), nil)
	working(t, e, s, "true")
	res, _ := e.stopUntilIdle(context.Background(), s, claudeStop)
	if res.Result != harness.ResultFailed || !strings.Contains(res.Message, "after 2 presses") {
		t.Fatalf("stop = %+v", res)
	}
	if got := pane.sent(); got != "Escape C-x+C-k+C-x+C-k Escape" {
		t.Fatalf("sent %q", got)
	}
}

// Stop with an approval waiting: Escape denies it and ends the turn, and
// the stop is done only once the approval is gone.
func TestStopDeniesWaitingApproval(t *testing.T) {
	const id = "s_stopappr01"
	pane := &stopPane{}
	approvals := &switchApprovals{}
	approvals.waiting.Store(true)
	e, s := stopEngine(t, id, pane, idleStatus(), approvals)
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); _, known := s.turnLocked(); return known })
	if e.idleNow(s) {
		t.Fatal("idle with an approval waiting")
	}
	pane.react = func(keys []string, _ int) {
		if keys[0] == "Escape" {
			approvals.waiting.Store(false)
		}
	}
	k := &spec.Keys{Keys: []string{"Escape"}, Until: spec.UntilIdle, Max: 2}
	res, _ := e.stopUntilIdle(context.Background(), s, k)
	if res.Result != harness.ResultApplied || pane.sent() != "Escape" {
		t.Fatalf("stop = %+v, sent %q", res, pane.sent())
	}
}

// With nothing running, Stop presses once, as the plain interrupt did,
// and sends no chord.
func TestStopWhenIdlePressesOnce(t *testing.T) {
	const id = "s_stopidle01"
	pane := &stopPane{}
	e, s := stopEngine(t, id, pane, idleStatus(), nil)
	working(t, e, s, "false")
	res, _ := e.stopUntilIdle(context.Background(), s, claudeStop)
	if res.Result != harness.ResultTyped || pane.sent() != "Escape" {
		t.Fatalf("stop = %+v, sent %q", res, pane.sent())
	}
}

// The hub says working for seconds after a turn ends. Once the mappers
// show the stop took, Working ends at once (the composer's Stop goes), and
// a new turn still shows.
func TestStopEndsWorkingBeforeTheHubSettles(t *testing.T) {
	const id = "s_stophub001"
	pane := &stopPane{}
	status := idleStatus()
	status.Store("working")
	e, s := stopEngine(t, id, pane, status, nil)
	working(t, e, s, "true")
	pane.react = func([]string, int) {
		deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	}
	k := &spec.Keys{Keys: []string{"Escape"}, Until: spec.UntilIdle, Max: 2}
	res, _ := e.stopUntilIdle(context.Background(), s, k)
	if res.Result != harness.ResultApplied || pane.sent() != "Escape" {
		t.Fatalf("stop = %+v, sent %q", res, pane.sent())
	}
	if s.sum.Load().Working {
		t.Fatal("still working after the stop while the hub settles")
	}
	working(t, e, s, "true")
}

// Then sends only the stop chords.
func TestStopRefusesOtherThenKeys(t *testing.T) {
	const id = "s_stopthen01"
	pane := &stopPane{}
	e, s := stopEngine(t, id, pane, idleStatus(), nil)
	working(t, e, s, "true")
	k := &spec.Keys{Keys: []string{"Escape"}, Until: spec.UntilIdle, Max: 2, Then: []string{"Enter"}}
	res, _ := e.stopUntilIdle(context.Background(), s, k)
	if res.Result != harness.ResultFailed || pane.sent() != "" {
		t.Fatalf("stop = %+v, sent %q", res, pane.sent())
	}
}
