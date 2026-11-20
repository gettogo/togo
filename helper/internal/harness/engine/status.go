package engine

import (
	"context"
	"time"
)

// statusChanged re-reads whether a turn runs once the hub's status moved.
// Working ORs in the hub, so a turn's last record can arrive while the hub
// still says working; without this the flag would stay set until the next
// record, showing Working, Interrupt and Queue on an idle session.
func (s *sess) statusChanged() {
	select {
	case <-s.ready:
	default:
		return
	}
	s.applyStatus()
}

// applyStatus is statusChanged once the session is ready.
func (s *sess) applyStatus() {
	if gone, at := s.statusChangedLocked(); gone {
		// Away from the session's lock: the store waits on an answer
		// being typed into the pane. Only what the dead agent asked
		// for: a relaunched agent may ask again in the meantime.
		if c, ok := s.e.cfg.Approvals.(exitedApprovals); ok {
			c.AgentExited(s.ctx, s.id, at)
		}
	}
}

// exitedApprovals is an approvals store that can close the approvals of
// an agent that exited (approval.Store).
type exitedApprovals interface {
	AgentExited(ctx context.Context, sessionID string, before time.Time)
}

// statusChangedLocked is statusChanged under the lock; it reports whether
// the agent is gone, and since when.
func (s *sess) statusChangedLocked() (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.gateOK {
		return false, time.Time{}
	}
	// A feed whose path comes from state (omp's session folder, found
	// once omp makes it) re-renders here too: waiting on a folder that
	// does not exist, it reads no record that would do it.
	s.syncFeedsLocked()
	gone := agentGone(s.hubStatus())
	if gone {
		if s.goneAt.IsZero() {
			s.goneAt = s.e.cfg.Now()
		}
		// Before the working flag falls: what the agent left open ends
		// interrupted, not as a turn that finished.
		s.agentExitedLocked()
	} else {
		s.goneAt = time.Time{}
		s.runningAgainLocked()
	}
	s.setWorkingLocked(s.stateLocked()["working"] == "true")
	if gone {
		s.exitNoticeLocked()
	} else {
		s.checkStuckTurnLocked()
		s.exitNoticed = false
	}
	return gone, s.goneAt
}
