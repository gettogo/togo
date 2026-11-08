package engine

import (
	"fmt"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
)

// A hook-fed agent whose API call fails may fire no hook for it: Auggie's
// turn stayed Working until the next prompt. The pane is the safety net.
// When the hooks still say a turn runs, the pane watcher says the agent is
// no longer working (its output settled, its working indicator is gone),
// no question is on screen and the screen shows an error, the turn ends as
// failed and the error becomes a notice. A new turn, or the hooks saying
// the turn ended, lifts the hold.

// hookFed reports whether any of the adapter's feeds is a hook.
func (s *sess) hookFed() bool {
	for _, f := range s.ad.Feeds {
		if f.Hook != nil {
			return true
		}
	}
	return false
}

// turnFailedLocked reports whether turn is the one the screen ended, and
// drops the hold once the mappers moved on: another turn, or none running.
func (s *sess) turnFailedLocked(turn string, mappersWorking bool) bool {
	if !s.failedHold {
		return false
	}
	if !mappersWorking || turn != s.failedTurn {
		s.failedHold = false
		return false
	}
	return true
}

// checkStuckTurnLocked looks at the screen, off the lock, when the hub
// stopped saying working while the hooks still do.
func (s *sess) checkStuckTurnLocked() {
	if s.failedHold || s.e.cfg.Screen == nil || !s.hookFed() || s.hubStatus() == "working" {
		return
	}
	if working, known := s.turnLocked(); !known || !working {
		return
	}
	go s.endStuckTurn()
}

func (s *sess) endStuckTurn() {
	screen := s.screenNow()
	if screen == "" || detect.Busy(s.info.Agent, screen) {
		return
	}
	if _, asking := detect.Detect(screen); asking {
		return
	}
	line, ok := screenError(screen)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failedHold || s.hubStatus() == "working" || agentGone(s.hubStatus()) {
		return
	}
	if working, _ := s.turnLocked(); !working {
		return
	}
	out, _ := s.mapperStateLocked()
	s.failedTurn, s.failedHold = out["turn"], true
	s.awaitReply = false
	now := s.e.cfg.Now()
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
	var ended []harness.Item
	for _, it := range recent {
		switch {
		case it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning:
			t := *it.Turn
			t.State, t.Ended = harness.StateFailed, &now
			it.Turn = &t
		case it.Kind == harness.KindTool && it.Tool != nil && it.Tool.State == harness.StateRunning:
			t := *it.Tool
			t.State, t.Ended = harness.StateInterrupted, &now
			it.Tool = &t
		default:
			continue
		}
		it.Seq = 0
		ended = append(ended, it)
	}
	ended = append(ended, harness.Item{ID: fmt.Sprintf("screenerr-%d", now.UnixMilli()), TurnID: s.failedTurn,
		Kind: harness.KindNotice, At: now, Notice: harness.ErrorNotice(harness.ClassifyError(line), line)})
	s.publishLocked(ended, nil)
	s.setWorkingLocked(s.stateLocked()["working"] == "true")
}
