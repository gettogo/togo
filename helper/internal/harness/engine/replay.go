package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/harness"
)

// restore maps an initial source snapshot under one lock. Historical item
// revisions and working transitions never escape as live notifications.
func (s *sess) restore(ctx context.Context, mapper string, rec harness.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	s.restoreLocked(ctx, mapper, rec)
	s.wantHistoryLocked(mapper, rec.Source)
}

// restoreLocked replays a snapshot into the mapper. It returns where in the
// snapshot the conversation the chat now shows begins: 0, or the record at which the snapshot moved to another one.
func (s *sess) restoreLocked(ctx context.Context, mapper string, rec harness.Record) int {
	from := 0
	m := s.mappers[mapper]
	if m == nil {
		return from
	}
	// Let existing clients show loading before replay starts. Their open waits
	// for this lock, so they still receive only the completed snapshot.
	s.notifyLocked(harness.Notification{Kind: harness.NoteReset, Reason: "history"})
	s.restoring = true
	defer func() { s.restoring = false }()
	for i := 0; i < len(rec.Replay); i++ {
		if ctx.Err() != nil {
			return from
		}
		one := harness.Record{Source: rec.Source, At: rec.At, Data: s.e.red.JSON(rec.Replay[i]), File: rec.File}
		if len(rec.ReplayAt) == len(rec.Replay) {
			one.At = rec.ReplayAt[i]
		}
		if mk, ok := m.(harness.ReplayMarker); ok && len(rec.ReplayMarks) == len(rec.Replay) {
			mk.Replaying(rec.ReplayMarks[i].Pre, rec.ReplayMarks[i].Post)
		}
		s.last[rec.Source] = one.Data
		items, ops := safeMap(m, one, s.e.cfg.Logf)
		if s.movedLocked() {
			// The snapshot names another conversation from here on (a file
			// the terminal moved to, a hook store spanning a /new): the chat
			// starts over and this record is mapped again by a fresh mapper,
			// which names its conversation as the one the chat shows. What
			// came before belongs to the conversation left.
			m = s.startOverLocked(mapper)
			from = i
			i--
			continue
		}
		s.followedLocked()
		s.publishLocked(items, ops)
		s.afterRecordLocked()
	}
	s.warnPlanLocked(m)
	s.restoring = false
	// The agent's files read again: a turn left open by an exit the hub
	// reported before they were, or one it never saw, ends now.
	if agentGone(s.hubStatus()) {
		s.agentExitedLocked()
		s.setWorkingLocked(s.stateLocked()["working"] == "true")
	} else {
		go s.checkRunStart()
	}
	s.storeSummaryLocked()
	s.summaryChangedLocked()
	if run := s.feeds[rec.Source]; run != nil {
		run.failures = 0
		s.recoverLocked(run)
	}
	return from
}
