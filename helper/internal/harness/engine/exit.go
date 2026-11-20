package engine

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
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
// reports another turn or the end of one, or the relaunched agent goes on
// with that same turn. It belongs to one conversation, and is kept under
// $TOGO_HOME/run/harness/<session>.exit (exit_store.go), since a restarted
// helper reads the agent's files again and its mappers find the dead turn
// open once more.
//
// Set: agentExitedLocked (the hub says the agent exited or failed, also
// when that was so before the session was served or replayed), and
// endRunLocked when the pane's agent process is younger than the turn the
// chat shows running (exit_run.go). An action's relaunch ends the old run
// too. Cleared: an id when a live record of the relaunched agent runs it
// again (the agent went on with it, or numbers its items afresh); a mask
// when a live record moves its mapper on or goes on with the masked turn
// (liftMaskLocked); the whole fence when the session is removed (Forget),
// and it stands aside while the chat shows another conversation.
type exitFence struct {
	At time.Time `json:"at"`
	// Run is when the agent was seen running again after the exit.
	Run time.Time `json:"run,omitzero"`
	// Conv is the conversation the chat showed at the exit.
	Conv string `json:"conv,omitempty"`
	// Items are the ids of the turns, tools, subagents and jobs the exit
	// ended.
	Items map[string]bool `json:"items"`
	// Rows are the subagent and job rows of mission control the exit
	// ended, by section and id ("background/7").
	Rows map[string]bool `json:"rows,omitempty"`
	// Masks maps a mapper to the turn it still said ran at the exit.
	Masks map[string]string `json:"masks,omitempty"`
}

// maxFenceItems bounds the ids a fence keeps over many exits.
const maxFenceItems = 1000

func rowKey(section, id string) string { return section + "/" + id }

// fenceNowLocked is the fence that applies to the conversation the chat
// shows, or nil.
func (s *sess) fenceNowLocked() *exitFence {
	if f := s.exit; f != nil && (f.Conv == "" || f.Conv == s.conv) {
		return f
	}
	return nil
}

// agentExitedLocked closes what the dead agent left open (endRunLocked)
// and withdraws the prompts queued behind it, since nothing will ever
// drain them. It runs before the working flag falls, so a mapper that
// closes its turn when the session goes idle (IdleCloser) closes it into
// the fence, interrupted.
func (s *sess) agentExitedLocked() {
	s.endRunLocked(s.e.cfg.Now())
	var ended []harness.Item
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
	if len(ended) > 0 {
		s.publishLocked(ended, nil)
	}
}

// endRunLocked ends what a run of the agent that is gone left open:
// running turns, tools, subagents and background jobs end as interrupted
// and open questions stop asking. It notes them in the session's fence,
// dated at.
func (s *sess) endRunLocked(at time.Time) {
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
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
	rows := s.mission.stopRunning()
	for _, op := range rows {
		ids[rowKey(op.Section, op.Row.ID)] = true
	}
	if len(ids) > 0 || len(masks) > 0 {
		s.fenceLocked(at, ids, masks)
	}
	for i := range ended {
		s.keepEndedLocked(&ended[i], false)
	}
	// The dead turn owes no reply: no "ended without a reply" for it.
	s.awaitReply = false
	ended = append(ended, s.unansweredLocked()...)
	if len(ended) > 0 || len(rows) > 0 {
		s.publishLocked(ended, rows)
	}
	// After the questions were published unanswerable: publishing an
	// open question counts it as asking again.
	s.forgetQuestionsLocked()
}

// promptsLocked reports whether items hold a prompt the chat has not shown,
// in a turn the exit did not end: the relaunched agent's next prompt.
func (s *sess) promptsLocked(items []harness.Item) bool {
	f := s.fenceNowLocked()
	if f == nil {
		return false
	}
	for _, it := range items {
		if it.Kind != harness.KindUser || it.Parent != "" || f.Items[it.TurnID] {
			continue
		}
		if _, ok := s.e.log.Get(s.id, it.ID); !ok {
			return true
		}
	}
	return false
}

// fenceLocked adds what an exit ended to the session's fence and saves it.
// An exit in another conversation than the fence's starts a new one.
func (s *sess) fenceLocked(at time.Time, ids map[string]bool, masks map[string]string) {
	f := s.exit
	if f == nil || f.Conv != s.conv || len(f.Items)+len(f.Rows)+len(ids) > maxFenceItems {
		f = &exitFence{Conv: s.conv, Items: map[string]bool{}, Masks: map[string]string{}}
		if s.exit != nil && s.exit.Conv == s.conv {
			f.Masks = s.exit.Masks
		}
		s.exit = f
	}
	if f.Masks == nil {
		f.Masks = map[string]string{}
	}
	if f.Rows == nil {
		f.Rows = map[string]bool{}
	}
	changed := false
	for id := range ids {
		into := f.Items
		if strings.HasPrefix(id, harness.SectionAgents+"/") || strings.HasPrefix(id, harness.SectionBackground+"/") {
			into = f.Rows
		}
		if !into[id] {
			into[id], changed = true, true
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
		f.At, f.Run = at, time.Time{}
	}
	s.saveFenceLocked()
}

// saveFenceLocked writes the fence away from the session's lock.
func (s *sess) saveFenceLocked() {
	if s.exit == nil {
		return
	}
	b, err := json.Marshal(s.exit)
	if err != nil {
		return
	}
	go s.e.saveExitFence(s.id, fenceSeq.Add(1), b)
}

// runningAgainLocked notes that the agent runs again after an exit: from
// now on its live records may go on with what the exit ended.
func (s *sess) runningAgainLocked() {
	if f := s.exit; f != nil && f.Run.IsZero() {
		f.Run = s.e.cfg.Now()
		s.saveFenceLocked()
	}
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
