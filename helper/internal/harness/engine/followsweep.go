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
// counts; a session list that cannot be read keeps everything. It runs in
// its own goroutine from New, so it is given the values it needs rather
// than reading the engine's config.
func sweepFollows(ctx context.Context, sessions Sessions, togoHome string, now func() time.Time) {
	if sessions == nil || togoHome == "" {
		return
	}
	for id, at := range source.FollowSessions(togoHome) {
		if now().Sub(at) < followGrace {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := sessions.Resolve(rctx, id)
		cancel()
		if errors.Is(err, session.ErrNotFound) {
			source.RemoveFollows(togoHome, id)
		}
	}
}
