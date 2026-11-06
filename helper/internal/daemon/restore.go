package daemon

import (
	"context"

	"github.com/gettogo/togo/helper/internal/session"
)

// restored follows session.restore: whatever the hub kept about the
// session's previous run is cleared, the session is published, and the
// watcher taps it now rather than at the next reconcile.
func (c *Components) restored(ctx context.Context, sess session.Session) session.Session {
	c.Hub.ClearExit(sess.ID)
	go c.Watch.Reconcile(context.WithoutCancel(ctx))
	return c.Hub.PublishSession(sess)
}
