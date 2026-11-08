package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Stop ends the whole turn, not just whatever the first key reaches. One
// Escape can leave work running: Claude Code's background agents keep the
// session busy at an idle prompt, where Escape does nothing. So an
// until-idle interrupt presses, sends the adapter's Then (the chord that
// stops background agents) once, then reads whether the session went idle
// (no turn, no approval waiting), and presses again only if not, a bounded
// number of times, like the mode cycle. At an approval the first Escape
// denies it and ends the turn.

// stopWait is how long one press has to leave the session idle (see
// stopped). It also spaces two presses, so a harness never reads them as
// a double press (Claude Code's Escape Escape clears the draft or opens
// rewind).
const stopWait = 5 * time.Second

// thenGap lets the harness take the press before Then arrives.
const thenGap = 300 * time.Millisecond

// maxStop bounds the presses of one stop whatever the adapter says.
const maxStop = 4

// stopUntilIdle presses k.Keys until the session is idle, at most k.Max
// times, sending k.Then once after the first press. The keys skip the
// typing lock: a stop never waits behind a write.
func (e *Engine) stopUntilIdle(ctx context.Context, s *sess, k *spec.Keys) (harness.ActionResult, error) {
	for _, key := range k.Then {
		if !spec.StopKeys[key] {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "That key is not allowed"}, nil
		}
	}
	if e.cfg.Typist == nil {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	if e.idleNow(s) {
		// Nothing runs that Togo can see: one press, which the harness
		// may still need (a turn the hooks have not reported yet).
		if err := e.cfg.Typist.Keys(ctx, s.id, k.Keys...); err != nil {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "Sending keys failed"}, nil
		}
		return harness.ActionResult{Result: harness.ResultTyped}, nil
	}
	max := min(k.Max, maxStop)
	for press := 1; press <= max; press++ {
		if err := e.cfg.Typist.Keys(ctx, s.id, k.Keys...); err != nil {
			return harness.ActionResult{Result: harness.ResultFailed, Message: "Sending keys failed"}, nil
		}
		if press == 1 && len(k.Then) > 0 {
			select {
			case <-ctx.Done():
				return harness.ActionResult{}, ctx.Err()
			case <-time.After(e.scaled(thenGap)):
			}
			if err := e.cfg.Typist.Keys(ctx, s.id, k.Then...); err != nil {
				return harness.ActionResult{Result: harness.ResultFailed, Message: "Sending keys failed"}, nil
			}
		}
		if e.awaitIdle(ctx, s) {
			e.holdStop(s)
			return harness.ActionResult{Result: harness.ResultApplied}, nil
		}
		if ctx.Err() != nil {
			return harness.ActionResult{}, ctx.Err()
		}
	}
	return harness.ActionResult{Result: harness.ResultFailed,
		Message: fmt.Sprintf("Still working after %d presses; stopped", max)}, nil
}

// stopped reports whether a stop is done: no approval waits and the
// mappers see no turn. The mappers end a turn as soon as its interrupt is
// written; the hub's status follows the pane and settles seconds later
// (longer while the pane redraws), so it is read only for a harness whose
// mappers never report a turn.
func (e *Engine) stopped(s *sess) bool {
	s.mu.Lock()
	working, known := s.turnLocked()
	s.mu.Unlock()
	if !known {
		return e.idleNow(s)
	}
	if working {
		return false
	}
	return e.cfg.Approvals == nil || len(e.cfg.Approvals.Pending(s.id)) == 0
}

// awaitIdle waits up to stopWait for the session to stay idle for
// idleSettle, so a turn boundary does not read as stopped.
func (e *Engine) awaitIdle(ctx context.Context, s *sess) bool {
	deadline := time.Now().Add(e.scaled(stopWait))
	var since time.Time
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(e.scaled(verifyPoll)):
		}
		if e.stopped(s) {
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= e.scaled(idleSettle) {
				return true
			}
		} else {
			since = time.Time{}
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

// stopHold is how long after a stop the hub's "working" is not read for a
// session whose mappers report turns: the hub follows the pane and says
// working for seconds after the turn ended (longer while the pane
// redraws), which kept Working and Stop on screen after a stop.
const stopHold = 15 * time.Second

// holdStop ends working now that a stop ended the turn, without waiting
// for the hub to settle.
func (e *Engine) holdStop(s *sess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stoppedAt = e.cfg.Now()
	s.awaitReply = false
	if s.gateOK {
		s.setWorkingLocked(s.stateLocked()["working"] == "true")
	}
}

// holdingStopLocked reports whether the hub's status is set aside: a stop
// ended the turn within stopHold and the mappers, which report turns,
// see none running. A new turn shows in the mappers at once.
func (s *sess) holdingStopLocked(states []map[string]string) bool {
	if s.stoppedAt.IsZero() || s.e.cfg.Now().Sub(s.stoppedAt) >= s.e.scaled(stopHold) {
		return false
	}
	for _, st := range states {
		if _, ok := st["working"]; ok {
			return !harness.MergeWorking(states...)
		}
	}
	return false
}
