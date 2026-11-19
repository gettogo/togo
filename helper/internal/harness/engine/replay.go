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

func (s *sess) restoreLocked(ctx context.Context, mapper string, rec harness.Record) {
	m := s.mappers[mapper]
	if m == nil {
		return
	}
	// Let existing clients show loading before replay starts. Their open waits
	// for this lock, so they still receive only the completed snapshot.
	s.notifyLocked(harness.Notification{Kind: harness.NoteReset, Reason: "history"})
	s.restoring = true
	defer func() { s.restoring = false }()
	moved := false
	for i := 0; i < len(rec.Replay); i++ {
		if ctx.Err() != nil {
			return
		}
		one := harness.Record{Source: rec.Source, At: rec.At, Data: s.e.red.JSON(rec.Replay[i]), File: rec.File}
		s.last[rec.Source] = one.Data
		items, ops := safeMap(m, one, s.e.cfg.Logf)
		if !moved && s.movedLocked() {
			// The file is another conversation's (the terminal moved to it):
			// the chat starts over and the snapshot is read from its start.
			moved = true
			m = s.startOverLocked(mapper)
			i = -1
			continue
		}
		s.publishLocked(items, ops)
		s.afterRecordLocked()
	}
	s.warnPlanLocked(m)
	s.restoring = false
	s.storeSummaryLocked()
	s.summaryChangedLocked()
	if run := s.feeds[rec.Source]; run != nil {
		run.failures = 0
		s.recoverLocked(run)
	}
}
