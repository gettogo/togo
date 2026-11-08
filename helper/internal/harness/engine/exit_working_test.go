package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// An agent that crashes or is killed mid-turn writes no turn end, so its
// mapper keeps saying working. Once the hub says the session ended or
// failed, working clears, the open turn ends as interrupted and prompts
// queued behind it are withdrawn instead of waiting forever.
func TestWorkingClearsWhenTheAgentExits(t *testing.T) {
	for _, exit := range []string{wire.StatusEnded, wire.StatusFailed, "exited"} {
		t.Run(exit, func(t *testing.T) {
			id := "s_exitwrk" + exit[:2] + "01"
			var status atomic.Value
			status.Store("working")
			e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
			s, _ := e.served(context.Background(), id)
			sub := &fakeSub{done: make(chan struct{})}
			defer close(sub.done)
			if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
				t.Fatal(err)
			}
			started := time.Now().Add(-time.Second)
			deliver(id, map[string]any{"state": map[string]string{"working": "true"},
				"items": []harness.Item{{ID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateRunning, Started: started}}}})
			waitFor(t, func() bool { return s.sum.Load().Working })
			queued := queue(t, e, id, "later")

			status.Store(exit)
			sid := id
			e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
				Session: session.Session{ID: id, Agent: "fake", Managed: true, Status: exit},
			}})
			waitFor(t, func() bool { return !s.sum.Load().Working })
			waitFor(t, func() bool {
				it, ok := e.log.Get(id, "t1")
				return ok && it.Turn.State == harness.StateInterrupted && it.Turn.Ended != nil
			})
			waitFor(t, func() bool {
				it, ok := e.log.Get(id, queued)
				return ok && it.User != nil && it.User.Withdrawn && !it.User.Queued
			})
			if n := s.queueLen(); n != 0 {
				t.Errorf("queue still holds %d", n)
			}
		})
	}
}
