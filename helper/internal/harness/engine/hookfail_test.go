package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// From the Auggie audit: an API call fails, no hook fires, and the turn
// stays Working. Once the pane settles with an error on screen the turn
// ends as failed with the error as a notice.
func TestHookTurnEndsOnScreenError(t *testing.T) {
	const id = "s_hookfail01"
	var status atomic.Value
	status.Store("working")
	pane := &dialogPane{}
	e := statusEngine(t, id, pane, &status, func(c *Config) { c.Screen = pane.Screen })
	s, _ := e.served(context.Background(), id)
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "true", "turn": "t1"},
		"items": []harness.Item{{ID: "t1", TurnID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateRunning}}}})
	waitFor(t, func() bool { return s.sum.Load().Working })

	pane.set("› hi\n\nError: 503 Service Unavailable: upstream connect error\n\n› \n")
	status.Store("idle")
	sid := id
	e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Agent: "fake", Managed: true, Status: "idle"},
	}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	waitFor(t, func() bool {
		it, ok := e.log.Get(id, "t1")
		return ok && it.Turn.State == harness.StateFailed
	})
	ns := noticesIn(e, id)
	if len(ns) != 1 || ns[0].Notice.Reason != harness.ErrProviderUnreachable || !strings.Contains(ns[0].Notice.Text, "503") {
		t.Fatalf("notices = %+v", ns)
	}
	// The pane redrawing (hub says working) does not bring Working back.
	status.Store("working")
	e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Agent: "fake", Managed: true, Status: "working"},
	}})
	if s.sum.Load().Working {
		t.Fatal("working came back for the failed turn")
	}
	// A new turn from the hooks runs normally.
	deliver(id, map[string]any{"state": map[string]string{"working": "true", "turn": "t2"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
}

// Without an error on screen a quiet pane is no failure.
func TestHookTurnStaysWithoutScreenError(t *testing.T) {
	const id = "s_hookfail02"
	var status atomic.Value
	status.Store("working")
	pane := &dialogPane{}
	pane.set("› thinking about it\n")
	e := statusEngine(t, id, pane, &status, func(c *Config) { c.Screen = pane.Screen })
	s, _ := e.served(context.Background(), id)
	deliver(id, map[string]any{"state": map[string]string{"working": "true", "turn": "t1"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	status.Store("idle")
	sid := id
	e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Agent: "fake", Managed: true, Status: "idle"},
	}})
	waitFor(t, func() bool { return s.queueLen() == 0 })
	if !s.sum.Load().Working || len(noticesIn(e, id)) != 0 {
		t.Fatalf("working = %v, notices = %d", s.sum.Load().Working, len(noticesIn(e, id)))
	}
}
