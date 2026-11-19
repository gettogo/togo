package enginetest

import (
	"context"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Hub sets the hub's status for the session and tells the engine, as the
// hub's session event does when the agent starts working, goes idle or
// exits ("working", "idle", "ended"; "" is unknown). Without the event the
// engine looks at the status only when a record arrives or on its next
// look.
func (r *Rig) Hub(status string) {
	r.SetStatus(status)
	info, err := sessions{r}.Resolve(context.Background(), r.ID)
	if err != nil {
		r.T.Fatalf("hub: %v", err)
	}
	id := r.ID
	r.Engine.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{Session: info}})
}
