package engine

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// readyWait is how long a prompt waits for the terminal to show it can
// take one; readyPoll is how often the screen is read meanwhile.
var (
	readyWait = 20 * time.Second
	readyPoll = 250 * time.Millisecond
)

// awaitReady waits for the adapter's ready row to show on screen before a
// prompt is typed. An agent the helper has just restarted (a model picked
// before the first prompt restarts OpenCode on the new session) draws its
// composer a few seconds later, and keys typed before that are lost: seen
// on OpenCode 2.2.5, where the first prompt after such a restart never
// arrived. With no ready row declared, or a screen that cannot be read,
// nothing is waited for. When the row never shows the prompt is typed
// anyway, as it was before.
func (e *Engine) awaitReady(ctx context.Context, s *sess) {
	if s.ad.Ready == "" || e.cfg.Screen == nil {
		return
	}
	re, err := regexp.Compile(s.ad.Ready)
	if err != nil {
		return
	}
	deadline := time.Now().Add(e.scaled(readyWait))
	for {
		if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
			for _, line := range strings.Split(screen, "\n") {
				if re.MatchString(line) {
					return
				}
			}
		}
		if !time.Now().Before(deadline) {
			e.cfg.Logf("harness: %s: the terminal did not show its input within %s; typing anyway", s.ad.Adapter.Adapter, readyWait)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.scaled(readyPoll)):
		}
	}
}
