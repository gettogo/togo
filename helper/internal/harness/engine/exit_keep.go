package engine

import (
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// turnMark names the turn a mapper's state says runs: its turn id, or
// when it reports none, when its working flag last changed.
func turnMark(st map[string]string) string {
	if t := st["turn"]; t != "" {
		return "turn:" + t
	}
	return "at:" + st[harness.StateWorkingAt]
}

// maskedLocked returns the mapper's state with working taken back while it
// is the dead turn's.
func (s *sess) maskedLocked(mapper string, st map[string]string) map[string]string {
	f := s.fenceNowLocked()
	if f == nil {
		return st
	}
	mark, ok := f.Masks[mapper]
	if !ok || st["working"] != "true" || turnMark(st) != mark {
		return st
	}
	out := make(map[string]string, len(st))
	for k, v := range st {
		out[k] = v
	}
	out["working"], out["idle"] = "false", "true"
	return out
}

// liftMaskLocked believes the mapper again once a live record moved it
// on (to another turn, or to a turn end), or, after the agent runs again,
// went on with the masked turn itself: a resumed turn, or tool calls in
// it with no new prompt. Only a live record does: a mapper being replayed
// after a helper restart, or not yet replayed, passes through other
// states on its way back to the dead turn.
func (s *sess) liftMaskLocked(mapper string, m harness.Mapper, items []harness.Item) {
	f := s.fenceNowLocked()
	if f == nil {
		return
	}
	mark, ok := f.Masks[mapper]
	if !ok {
		return
	}
	if st := m.State(); st["working"] != "true" || turnMark(st) != mark {
		delete(f.Masks, mapper)
		return
	}
	if f.Run.IsZero() {
		return
	}
	turn, named := strings.CutPrefix(mark, "turn:")
	for _, it := range items {
		if !named || it.TurnID == turn {
			delete(f.Masks, mapper)
			if named {
				s.reviveTurnLocked(turn)
			}
			return
		}
	}
}

// keepEndedRowsLocked holds a row the exit ended at interrupted when a
// mapper (replayed after a restart, or never told of the exit) lists it
// running again.
func (s *sess) keepEndedRowsLocked(ops []harness.Op) []harness.Op {
	f := s.fenceNowLocked()
	if f == nil || len(f.Rows) == 0 {
		return ops
	}
	var out []harness.Op
	for i, op := range ops {
		if op.Op != harness.OpUpsert || op.Row == nil || !f.Rows[rowKey(op.Section, op.Row.ID)] ||
			(op.Row.State != harness.StateRunning && op.Row.State != harness.StateWaiting) {
			continue
		}
		if !s.restoring && !f.Run.IsZero() {
			delete(f.Rows, rowKey(op.Section, op.Row.ID)) // the relaunched agent's own
			continue
		}
		if out == nil {
			out = slices.Clone(ops)
		}
		row := *op.Row
		row.State = harness.StateInterrupted
		out[i].Row = &row
	}
	if out == nil {
		return ops
	}
	return out
}

// keepEndedLocked holds an item the exit ended at interrupted: a mapper
// that closes the dead turn when the relaunched agent's next prompt comes
// would show it finished, and one replayed after a restart would show it
// running. Anything running in a dead turn ended with it.
//
// Three things let an item go. An end the agent wrote before it exited,
// which the feed delivered late, stands: it is undated or dated before
// the exit, and comes without a new prompt (prompted says the batch holds
// one); a turn that ended so gets its "finished" push. And a live record
// of the agent running again that runs the item again: the agent went on
// with it, or numbers its items afresh.
func (s *sess) keepEndedLocked(it *harness.Item, prompted bool) {
	f := s.fenceNowLocked()
	if f == nil || (!f.Items[it.ID] && !f.Items[it.TurnID]) {
		return
	}
	own := f.Items[it.ID] || (it.Kind == harness.KindTurn && f.Items[it.TurnID])
	again := !s.restoring && !f.Run.IsZero()
	end := func(state string, ended *time.Time) (string, *time.Time, bool) {
		switch {
		case state == harness.StateInterrupted:
			return state, ended, false
		case state == harness.StateRunning && again:
			delete(f.Items, it.ID)
			if f.Items[it.TurnID] {
				s.reviveTurnLocked(it.TurnID)
			}
			return state, ended, false
		case state == harness.StateRunning:
		case !own:
			return state, ended, false
		case !prompted && (ended == nil || ended.Before(f.At)):
			delete(f.Items, it.ID)
			if it.Kind == harness.KindTurn {
				delete(f.Items, it.TurnID)
				if state == harness.StateOK && !s.restoring {
					s.lateDone = true
				}
			}
			return state, ended, false
		}
		at := f.At
		return harness.StateInterrupted, &at, true
	}
	switch {
	case it.Kind == harness.KindTurn && it.Turn != nil:
		t := *it.Turn
		var ok bool
		if t.State, t.Ended, ok = end(t.State, t.Ended); ok {
			it.Turn = &t
		}
	case it.Kind == harness.KindTool && it.Tool != nil:
		t := *it.Tool
		var ok bool
		if t.State, t.Ended, ok = end(t.State, t.Ended); ok {
			it.Tool = &t
		}
	case it.Kind == harness.KindAgent && it.Agent != nil:
		a := *it.Agent
		var ok bool
		if a.State, a.Ended, ok = end(a.State, a.Ended); ok {
			it.Agent = &a
		}
	case it.Kind == harness.KindBackground && it.Background != nil:
		b := *it.Background
		var ok bool
		if b.State, b.Ended, ok = end(b.State, b.Ended); ok {
			it.Background = &b
		}
	}
}

// reviveTurnLocked takes a turn the relaunched agent went on with out of
// the fence: its masks lift, and its turn item runs again (published by
// afterKeepLocked).
func (s *sess) reviveTurnLocked(turn string) {
	f := s.fenceNowLocked()
	if f == nil {
		return
	}
	delete(f.Items, turn)
	for name, mark := range f.Masks {
		if mark == "turn:"+turn {
			delete(f.Masks, name)
		}
	}
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
	for _, it := range recent {
		if it.Kind != harness.KindTurn || it.Turn == nil || it.TurnID != turn || it.Turn.State != harness.StateInterrupted {
			continue
		}
		delete(f.Items, it.ID)
		t := *it.Turn
		t.State, t.Ended = harness.StateRunning, nil
		it.Turn, it.Seq = &t, 0
		s.revive = append(s.revive, it)
	}
	s.saveFenceLocked()
}

// afterKeepLocked publishes the turns a batch revived and sends the
// "finished" push of a turn whose end came after the exit.
func (s *sess) afterKeepLocked() {
	if len(s.revive) > 0 {
		r := s.revive
		s.revive = nil
		s.publishLocked(r, nil)
	}
	if s.lateDone {
		s.lateDone = false
		s.saveFenceLocked()
		s.finishedLocked()
	}
}
