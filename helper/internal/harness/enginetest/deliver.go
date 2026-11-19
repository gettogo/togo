package enginetest

import (
	"context"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// DeliverAt is Deliver with the time the hook fired, so the chat's times
// follow a generated session's clock and not the test's.
func (r *Rig) DeliverAt(event string, payload []byte, at time.Time) {
	source.Hooks.Deliver(r.ID, source.Payload{Kind: source.BusHook, Event: event, At: at, Data: payload})
}

// Cursor is the Seq of the newest item the engine holds for the session.
// It asks for the items after a Seq no log has reached, so it stays cheap
// as the log grows.
func (r *Rig) Cursor() int64 {
	r.T.Helper()
	sub := newSub()
	defer close(sub.done)
	res, err := r.Engine.Open(context.Background(), sub, r.ID, 1<<60, 1)
	if err != nil {
		r.T.Fatalf("open: %v", err)
	}
	_ = r.Engine.Close(context.Background(), sub, r.ID)
	return res.Cursor
}

// WaitCursor waits until the newest Seq passes after, for a hook that
// usually draws an item: the engine has then read it. It gives up after
// limit and says whether it saw the Seq pass.
func (r *Rig) WaitCursor(after int64, limit time.Duration) bool {
	r.T.Helper()
	deadline := time.Now().Add(limit)
	for r.Cursor() <= after {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Microsecond)
	}
	return true
}

// hookGrace is how long WaitHook waits after the hook readers are idle
// before it says the hook drew nothing: a record held while the history
// loads is drawn a little later.
const hookGrace = 500 * time.Millisecond

// hooksIdle reports that no goroutine reading hook payloads is working on
// one: each is waiting for the next. A payload sent to a reader that waits
// makes it runnable at once, so after Deliver returns an idle reader has
// finished the payload.
func hooksIdle() bool {
	for _, g := range stacks() {
		if !strings.Contains(g, "harness/source.Hook.Watch") {
			continue
		}
		state := g[strings.Index(g, "[")+1:]
		state = state[:strings.IndexAny(state, ",]")]
		if state != "select" && state != "chan receive" {
			return false
		}
	}
	return true
}

// WaitHook waits for the engine to draw what a delivered hook draws: it
// returns true once the newest Seq passes after. A hook that draws nothing
// (a duplicate, a stop after a restart) never moves it, so the wait also
// ends, with false, once the hook readers have been idle for hookGrace
// without it moving. limit bounds the wait either way.
func (r *Rig) WaitHook(after int64, limit time.Duration) bool {
	r.T.Helper()
	begin := time.Now()
	deadline := begin.Add(limit)
	var idleSince time.Time
	for r.Cursor() <= after {
		now := time.Now()
		if now.After(deadline) {
			return false
		}
		switch {
		case now.Sub(begin) < 5*time.Millisecond:
			// Most hooks draw within this: no need to read the stacks.
		case !hooksIdle():
			idleSince = time.Time{}
		case idleSince.IsZero():
			idleSince = now
		case now.Sub(idleSince) >= hookGrace:
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// SetEnded tells the engine the agent's process ended (true) or runs
// again (false), as the hub's session event does when the pane's program
// exits or a relaunch starts it.
func (r *Rig) SetEnded(ended bool) {
	status := "working"
	if ended {
		status = wire.StatusEnded
	}
	r.SetStatus(status)
	id := r.ID
	r.Engine.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Agent: r.Opt.Agent, Managed: true, Status: status}}})
}
