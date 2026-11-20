package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/panelock"
)

// paneSeq takes the session's pane for one typing sequence (clear, paste,
// Enter; a cycle of keys; a menu command), so an answer or a prompt from
// another path cannot land between its keys. It is taken under the typing
// lock and never under the session lock. The typist calls given the
// returned context take nothing more.
func (e *Engine) paneSeq(ctx context.Context, s *sess) (context.Context, func(), error) {
	return panelock.Acquire(ctx, s.id)
}

// stopKeys sends a stop's keys at once, past any write holding the pane;
// the write sends no further key (panelock.Stop).
func (e *Engine) stopKeys(ctx context.Context, s *sess, keys ...string) error {
	if k, ok := e.cfg.Typist.(interface {
		StopKeys(ctx context.Context, id string, keys ...string) error
	}); ok {
		return k.StopKeys(ctx, s.id, keys...)
	}
	return e.cfg.Typist.Keys(ctx, s.id, keys...)
}
