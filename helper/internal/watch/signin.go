package watch

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
)

// A login command can wait on the user under a spinner ("⣾ Waiting for
// authorization..."), so its output never falls silent and the idle check
// that finds prompts never runs. While a program without a known working
// indicator keeps drawing in the first minutes after its tap attached,
// which is when login commands and first-run sign-ins wait, its screen is
// read every signInCheckEvery for a sign-in step only.
const (
	signInCheckEvery = 3 * time.Second
	signInWindow     = 10 * time.Minute
)

// signInCheck reports a sign-in step found on a working session's screen.
// It reports true when it found one, so the tick does nothing else.
func (w *Watcher) signInCheck(ctx context.Context, id string, e *entry, now time.Time) bool {
	if !e.working || e.prompt != "" || detect.KnowsBusy(e.agent) || e.attachedAt.IsZero() ||
		now.Sub(e.attachedAt) > signInWindow || now.Sub(e.lastSignInCheck) < signInCheckEvery {
		return false
	}
	e.lastSignInCheck = now
	p, ok, err := w.inspect(ctx, id)
	if err != nil || !ok || p.Kind != detect.KindSignIn {
		return false
	}
	w.promptSeen(ctx, id, e, p)
	return true
}
