package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Adapter actions that start the harness's own session on demand.
const (
	// actionStartSession creates the harness's session (OpenCode and Kilo
	// make one only with the first prompt).
	actionStartSession = "start_session"
	// actionShowSession puts the session just started on the agent's
	// screen, so the desk shows what the phone drives.
	actionShowSession = "show_session"
)

// sessionStartWait bounds the wait for the feed to report the session the
// start action created.
var sessionStartWait = 8 * time.Second

// startSession starts the harness's session when a setting is picked
// before there is one, so the pick applies to the session the first prompt
// then runs in, rather than failing: the adapter's start_session action
// creates it, the feed names it (harness_session), and show_session, when
// the adapter has one, shows it in the agent's own screen once the pick
// applied (showSession). It reports whether a session is known now.
func (e *Engine) startSession(ctx context.Context, s *sess) bool {
	start, ok := s.ad.Actions[actionStartSession]
	if !ok {
		return false
	}
	s.starting.Lock()
	defer s.starting.Unlock()
	if s.sessionKnown() {
		return true // another pick started it meanwhile
	}
	res, err := e.run(ctx, s, actionStartSession, start, map[string]any{})
	if err != nil || res.Result != harness.ResultApplied {
		e.cfg.Logf("harness: %s: starting its session for a setting: %v %s %s", s.ad.Adapter.Adapter, err, res.Result, res.Message)
		return false
	}
	deadline := time.Now().Add(sessionStartWait)
	for !s.sessionKnown() {
		if time.Now().After(deadline) || ctx.Err() != nil {
			e.cfg.Logf("harness: %s: the started session never reached the feed", s.ad.Adapter.Adapter)
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

// showSession puts the session just started on the agent's screen, when
// the adapter knows how; a failure leaves the desk on its own view.
func (e *Engine) showSession(ctx context.Context, s *sess) {
	show, ok := s.ad.Actions[actionShowSession]
	if !ok {
		return
	}
	if res, err := e.run(ctx, s, actionShowSession, show, map[string]any{}); err != nil || (res.Result != harness.ResultApplied && res.Result != harness.ResultRelaunched) {
		e.cfg.Logf("harness: %s: showing the started session: %v %s %s", s.ad.Adapter.Adapter, err, res.Result, res.Message)
	}
}

// sessionKnown reports whether the feed has named the harness's session.
func (s *sess) sessionKnown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()["harness_session"] != ""
}
