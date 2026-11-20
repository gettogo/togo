package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/procs"
)

// processStarted is when a live process started; a variable for tests.
var processStarted = procs.Started

// runSlack is how much older than the pane's process a turn must be to be
// taken for a dead run's: the clocks are the same host's, but a turn's
// time can be the agent's own rounding.
const runSlack = 2 * time.Second

// checkRunStart ends a turn the chat shows running when the agent's pane
// process started after it: a turn cannot run in a process younger than
// itself. This catches an exit the helper never saw, because it was down
// for both the exit and the relaunch, or the hub's word of it was lost. A
// live record the mappers read since the process started means it may be
// the new run's own turn, and the check stands aside. Runs off the
// session's lock: the process lookup is a system call per pane.
func (s *sess) checkRunStart() {
	if s.e.cfg.PanePIDs == nil || s.ad == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	pids, err := s.e.cfg.PanePIDs(ctx, s.id)
	cancel()
	if err != nil || len(pids) == 0 {
		return
	}
	start, err := processStarted(pids[0])
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || !s.gateOK || agentGone(s.hubStatus()) {
		return // the hub's exit is handled on its own path
	}
	if !s.liveAt.IsZero() && !s.liveAt.Before(start) {
		return
	}
	if !s.turnBeforeLocked(start.Add(-runSlack)) {
		return
	}
	s.e.cfg.Logf("harness: %s: the turn shown running started before the agent's process; it is ended", s.id)
	s.endRunLocked(start)
	s.runningAgainLocked()
	s.setWorkingLocked(s.stateLocked()["working"] == "true")
}

// turnBeforeLocked reports whether a turn the chat or a mapper shows
// running began before t.
func (s *sess) turnBeforeLocked(t time.Time) bool {
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
	for _, it := range recent {
		if it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning &&
			!it.Turn.Started.IsZero() && it.Turn.Started.Before(t) {
			return true
		}
	}
	for name, m := range s.mappers {
		st := s.maskedLocked(name, m.State())
		if st["working"] != "true" {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, st[harness.StateWorkingAt]); err == nil && at.Before(t) {
			return true
		}
	}
	return false
}
