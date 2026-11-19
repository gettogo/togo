package enginetest

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Hub sets the hub's status for the session and tells the engine, as the
// hub's session event does when the agent starts working, goes idle or
// exits ("working", "idle", "ended"; "" is unknown). Without the event the
// engine looks at the status only when a record arrives or on its next
// look.
func (r *Rig) Hub(status string) {
	r.SetStatus(status)
	info, err := sessions{r}.Resolve(context.Background(), SessionID)
	if err != nil {
		r.T.Fatalf("hub: %v", err)
	}
	id := SessionID
	r.Engine.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{Session: info}})
}

// Remove tells the engine the session was removed, as the hub does when
// the user removes it, and waits until the engine has dropped it.
func (r *Rig) Remove() {
	id := SessionID
	r.Engine.Observe(wire.Event{Kind: wire.EventSessionRemoved, SessionID: &id, Payload: wire.SessionRemovedEvent{}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := r.Engine.Summary(id); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.T.Fatalf("remove: the engine still serves the session")
}
