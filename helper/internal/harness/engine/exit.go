package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// exitFence is what an agent's exit ended. The mappers never learn of the
// exit: a crashed or killed agent writes no turn end, so the turn it died
// in stays open in them, and their state goes on saying a turn runs. Once
// the agent runs again (relaunched from the phone or by hand), that state
// would count again and the session read working for ever; and the
// relaunched agent's next prompt in the same conversation makes some
// mappers close the dead turn as if it had finished.
//
// The fence holds the ids of the items the exit ended, so they stay ended
// whatever a mapper later says of them, and for each mapper that said a
// turn ran, which turn that was: its "working" is not believed until it
// reports another turn or the end of one. It is kept under
// $TOGO_HOME/run/harness/<session>.exit, since a restarted helper reads the
// agent's files again and its mappers find the dead turn open once more.
//
// Set: agentExitedLocked (the hub says the agent exited or failed).
// Cleared: a mapper's mask when a live record shows it on another turn or
// at a turn end (liftMaskLocked); the whole fence when the session is removed
// (Forget). A relaunch keeps it: the dead turn stays dead.
type exitFence struct {
	At time.Time `json:"at"`
	// Items are the ids of the turns, tools, subagents and jobs the exit
	// ended.
	Items map[string]bool `json:"items"`
	// Masks maps a mapper to the turn it still said ran at the exit.
	Masks map[string]string `json:"masks,omitempty"`
}

// maxFenceItems bounds the ids a fence keeps over many exits.
const maxFenceItems = 1000

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
	f := s.exit
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
// on: to another turn, or to a turn end. Only a live record does: a
// mapper being replayed after a helper restart, or not yet replayed,
// passes through other states on its way back to the dead turn.
func (s *sess) liftMaskLocked(mapper string, m harness.Mapper) {
	f := s.exit
	if f == nil {
		return
	}
	mark, ok := f.Masks[mapper]
	if !ok {
		return
	}
	if st := m.State(); st["working"] != "true" || turnMark(st) != mark {
		delete(f.Masks, mapper)
	}
}

// keepEndedLocked holds an item the exit ended at interrupted: a mapper
// that closes the dead turn when the relaunched agent's next prompt comes
// would show it finished, and one replayed after a restart would show it
// running. An end the agent wrote before it exited and the feed delivered
// late stands: it is dated before the exit and comes without a new prompt
// (prompted says the batch holds one). Anything still running in a dead
// turn ended with it.
func (s *sess) keepEndedLocked(it *harness.Item, prompted bool) {
	f := s.exit
	if f == nil || (!f.Items[it.ID] && !f.Items[it.TurnID]) {
		return
	}
	own := f.Items[it.ID] || (it.Kind == harness.KindTurn && f.Items[it.TurnID])
	end := func(state string, ended *time.Time) (string, *time.Time, bool) {
		switch {
		case state == harness.StateInterrupted:
			return state, ended, false
		case state == harness.StateRunning:
		case !own:
			return state, ended, false
		case !prompted && ended != nil && ended.Before(f.At):
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

// agentExitedLocked closes what the dead agent left open: running turns,
// tools, subagents and background jobs end as interrupted, prompts queued
// behind them are withdrawn, since nothing will ever drain them, and open
// questions stop asking. It notes them in the session's exit fence. It runs
// before the working flag falls, so a mapper that closes its turn when the
// session goes idle (IdleCloser) closes it into the fence, interrupted.
func (s *sess) agentExitedLocked() {
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
	now := s.e.cfg.Now()
	var ended []harness.Item
	ids := map[string]bool{}
	for _, it := range recent {
		switch {
		case it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning:
		case it.Kind == harness.KindTool && it.Tool != nil && it.Tool.State == harness.StateRunning:
		case it.Kind == harness.KindAgent && it.Agent != nil && it.Agent.State == harness.StateRunning:
		case it.Kind == harness.KindBackground && it.Background != nil && it.Background.State == harness.StateRunning:
		default:
			continue
		}
		ids[it.ID] = true
		it.Seq = 0
		ended = append(ended, it)
	}
	masks := map[string]string{}
	for name, m := range s.mappers {
		if st := m.State(); st["working"] == "true" {
			masks[name] = turnMark(st)
			if t := st["turn"]; t != "" {
				// A turn the mapper has not shown yet (one it draws when
				// it ends) is the dead turn too.
				ids[t] = true
			}
		}
	}
	if len(ids) > 0 || len(masks) > 0 {
		s.fenceLocked(now, ids, masks)
	}
	for i := range ended {
		s.keepEndedLocked(&ended[i], false)
	}
	rows := s.mission.stopRunning()
	for _, q := range s.queue {
		if q.item.User == nil {
			continue
		}
		it := q.item
		u := *it.User
		u.Queued, u.Withdrawn = false, true
		it.User = &u
		ended = append(ended, it)
	}
	s.queue = nil
	// The dead turn owes no reply: no "ended without a reply" for it.
	s.awaitReply = false
	asked := s.unansweredLocked()
	ended = append(ended, asked...)
	if len(ended) > 0 || len(rows) > 0 {
		s.publishLocked(ended, rows)
	}
	s.forgetQuestionsLocked()
}

// promptsLocked reports whether items hold a prompt the chat has not shown,
// in a turn the exit did not end: the relaunched agent's next prompt.
func (s *sess) promptsLocked(items []harness.Item) bool {
	if s.exit == nil {
		return false
	}
	for _, it := range items {
		if it.Kind != harness.KindUser || it.Parent != "" || s.exit.Items[it.TurnID] {
			continue
		}
		if _, ok := s.e.log.Get(s.id, it.ID); !ok {
			return true
		}
	}
	return false
}

// fenceLocked adds what an exit ended to the session's fence and saves it.
func (s *sess) fenceLocked(at time.Time, ids map[string]bool, masks map[string]string) {
	f := s.exit
	if f == nil || len(f.Items)+len(ids) > maxFenceItems {
		f = &exitFence{Items: map[string]bool{}, Masks: map[string]string{}}
		if s.exit != nil {
			f.Masks = s.exit.Masks
		}
		s.exit = f
	}
	if f.Masks == nil {
		f.Masks = map[string]string{}
	}
	changed := false
	for id := range ids {
		if !f.Items[id] {
			f.Items[id], changed = true, true
		}
	}
	for name, mark := range masks {
		if f.Masks[name] != mark {
			f.Masks[name], changed = mark, true
		}
	}
	if !changed {
		return
	}
	if len(ids) > 0 || f.At.IsZero() {
		f.At = at
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	go s.e.saveExitFence(s.id, fenceSeq.Add(1), b)
}

// closeIdleLocked publishes what IdleCloser mappers close once the session
// went idle.
func (s *sess) closeIdleLocked() {
	now := s.e.cfg.Now()
	for _, m := range s.mappers {
		if c, ok := m.(harness.IdleCloser); ok {
			if items := c.CloseIdle(now); len(items) > 0 {
				s.publishLocked(items, nil)
			}
		}
	}
}

// fenceSeq numbers fence writes, taken under the session's lock.
var fenceSeq atomic.Uint64

// fenceSaves orders the writes of each session's fence: a write is
// skipped when a later one already landed.
var fenceSaves struct {
	sync.Mutex
	seq map[string]uint64
}

func (e *Engine) exitFencePath(id string) string {
	if e.cfg.TogoHome == "" {
		return ""
	}
	if _, err := session.ValidateID(id); err != nil {
		return ""
	}
	return filepath.Join(e.cfg.TogoHome, "run", "harness", id+".exit")
}

// saveExitFence writes the fence away from the session's lock.
func (e *Engine) saveExitFence(id string, seq uint64, b []byte) {
	path := e.exitFencePath(id)
	if path == "" {
		return
	}
	fenceSaves.Lock()
	defer fenceSaves.Unlock()
	if fenceSaves.seq == nil {
		fenceSaves.seq = map[string]uint64{}
	}
	if fenceSaves.seq[path] >= seq {
		return
	}
	fenceSaves.seq[path] = seq
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		e.cfg.Logf("harness: %s: the exit could not be kept: %v", id, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		e.cfg.Logf("harness: %s: the exit could not be kept: %v", id, err)
		return
	}
	_ = os.Rename(tmp, path)
}

// loadExitFence reads a fence a previous run of the helper kept.
func (e *Engine) loadExitFence(id string) *exitFence {
	path := e.exitFencePath(id)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f exitFence
	if json.Unmarshal(b, &f) != nil || len(f.Items) > maxFenceItems || len(f.Masks) > 64 {
		return nil
	}
	if f.Items == nil {
		f.Items = map[string]bool{}
	}
	return &f
}

// forgetExitFence removes the kept fence with its session.
func (e *Engine) forgetExitFence(id string) {
	path := e.exitFencePath(id)
	if path == "" {
		return
	}
	fenceSaves.Lock()
	defer fenceSaves.Unlock()
	if fenceSaves.seq == nil {
		fenceSaves.seq = map[string]uint64{}
	}
	// A write still on its way must not bring the file back.
	fenceSaves.seq[path] = fenceSeq.Add(1)
	_ = os.Remove(path)
}
