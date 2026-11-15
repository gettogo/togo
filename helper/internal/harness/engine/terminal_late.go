package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
)

// A command answered "typed" showed nothing in the time waited. The harness
// may still open its list (a slow fetch: Kimi's models, an extension's
// dialog). The screen is watched for it for a bounded time. A list that
// appears is recorded as left open, so nothing is typed into it, and the
// chat that ran the command is told once, by the request id it gave, so it
// can take the list up. The watch never turns "typed" into "done"; when it
// ends, it has only stopped looking.

type requestIDKey struct{}

type lateListWatch struct {
	cancel context.CancelFunc
}

const lateWatchEvery = 700 * time.Millisecond

func (e *Engine) stopLateWatch(session string) {
	e.menuMu.Lock()
	w := e.lateWatches[session]
	delete(e.lateWatches, session)
	e.menuMu.Unlock()
	if w != nil {
		w.cancel()
	}
}

// watchLateList starts the watch for the command ctx is running.
func (e *Engine) watchLateList(ctx context.Context, s *sess) {
	if e.cfg.StyledScreen == nil {
		return
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	conv := menuConv(ctx, s)
	watch, cancel := context.WithTimeout(context.Background(), e.scaled(lateWatch))
	e.menuMu.Lock()
	if e.lateWatches == nil {
		e.lateWatches = map[string]*lateListWatch{}
	}
	if old := e.lateWatches[s.id]; old != nil {
		old.cancel()
	}
	w := &lateListWatch{cancel: cancel}
	e.lateWatches[s.id] = w
	e.menuMu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			e.menuMu.Lock()
			if e.lateWatches[s.id] == w {
				delete(e.lateWatches, s.id)
			}
			e.menuMu.Unlock()
		}()
		for {
			select {
			case <-watch.Done():
				return
			case <-time.After(e.scaled(lateWatchEvery)):
			}
			// A restart, another conversation or a turn that began ends
			// the watch: what the screen shows is no longer this command's.
			if !e.stillServed(s, conv) || !e.idleNow(s) {
				return
			}
			screen, err := e.cfg.StyledScreen(watch, s.id)
			if err != nil {
				continue
			}
			menu, open := termmenu.Read(screen)
			if !open || menu.Title == "" {
				continue
			}
			e.menuMu.Lock()
			if e.leftOpen == nil {
				e.leftOpen = map[string]string{}
			}
			e.leftOpen[s.id] = menu.Title
			e.menuMu.Unlock()
			if requestID != "" {
				s.mu.Lock()
				s.notifyLocked(harness.Notification{Kind: harness.NoteMenu, RequestID: requestID})
				s.mu.Unlock()
			}
			return
		}
	}()
}
