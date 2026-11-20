package engine

import (
	"context"
	"errors"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// followGrace keeps a follow file of a session the session list does not
// name yet: one created just now.
const followGrace = time.Minute

// sweepFollows deletes what is remembered under run/follow-<session>.*
// for sessions that no longer exist: ones removed while the helper was
// not running (Forget removes the rest). Only a definite "not found"
// counts; a session list that cannot be read keeps everything.
func (e *Engine) sweepFollows(ctx context.Context) {
	if e.cfg.Sessions == nil || e.cfg.TogoHome == "" {
		return
	}
	for id, at := range source.FollowSessions(e.cfg.TogoHome) {
		if e.cfg.Now().Sub(at) < followGrace {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := e.cfg.Sessions.Resolve(rctx, id)
		cancel()
		if errors.Is(err, session.ErrNotFound) {
			source.RemoveFollows(e.cfg.TogoHome, id)
		}
	}
}
