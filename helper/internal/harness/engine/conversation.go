package engine

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// StateConversation is the mapper state key naming the conversation the
// agent's terminal shows. A mapper sets it when its harness can leave one
// conversation for another under a running agent (/new, /resume, a fork).
// When it changes, the chat starts over on the new conversation: a new log
// epoch, a fresh mapper and a fresh mission, so nothing of the old
// conversation stays in the chat and nothing of the old turn's state
// (tools, plan, usage, working, held prompts) carries into the new one.
const StateConversation = "conversation"

// movedLocked reports whether the mappers now name a conversation other
// than the one the chat shows. The first one named is the one the session
// started on.
func (s *sess) movedLocked() bool {
	id := s.stateLocked()[StateConversation]
	if id == "" || id == s.conv {
		return false
	}
	if s.conv == "" {
		s.conv = id
		return false
	}
	return true
}

// startOverLocked empties the chat for the conversation the agent moved to
// and returns a fresh mapper in place of the one that reported the move;
// the caller maps the announcing record again with it. Feeds keep running,
// so no record is missed, and open chats are told to reset and read the
// new epoch. Mappers of other feeds (settings, the status line) describe
// the agent, not the conversation, and stay.
//
// Prompts held for the old conversation are not sent to the new one: a
// notice in the new chat carries their text.
func (s *sess) startOverLocked(mapper string) harness.Mapper {
	var held []string
	for _, q := range s.queue {
		if q.item.User != nil {
			held = append(held, q.item.User.Text)
		}
	}
	s.e.log.Forget(s.id)
	draining.Delete(s.id)
	fresh := s.e.mapperFor(mapper, s.env)
	s.mappers[mapper] = fresh
	s.conv = ""
	s.queue, s.sent, s.phone = nil, map[string]bool{}, nil
	s.anchors = map[string]*harness.Anchor{}
	s.pending, s.sentText, s.lastPart = map[string]harness.Item{}, map[string]string{}, map[string]time.Time{}
	s.mission, s.srcRows = newMission(s.missionCaps()), nil
	s.needs = needsState{}
	s.turn, s.working, s.feedWorking = "", false, false
	s.stoppedAt, s.feedEndedAt = time.Time{}, time.Time{}
	s.awaitReply, s.awaitText = false, ""
	s.failedTurn, s.failedHold = "", false
	s.planWarned = false
	was := s.restoring
	s.restoring = false
	s.notifyLocked(harness.Notification{Kind: harness.NoteReset, Reason: "epoch"})
	s.unsentNoticeLocked(held)
	s.restoring = was
	return fresh
}

// unsentNoticeLocked tells the new conversation's chat which held prompts
// were dropped by the move.
func (s *sess) unsentNoticeLocked(held []string) {
	if len(held) == 0 {
		return
	}
	parts := make([]string, len(held))
	for i, text := range held {
		parts[i] = clipNotice(text)
	}
	text := s.ad.Name + " moved to another conversation, so "
	if len(held) == 1 {
		text += "a waiting prompt was not sent: "
	} else {
		text += "waiting prompts were not sent: "
	}
	text += strings.Join(parts, " | ")
	s.publishLocked([]harness.Item{{ID: "unsent-" + s.e.newID(), Kind: harness.KindNotice, At: s.e.cfg.Now(),
		Notice: &harness.NoticeBody{Level: "error", Code: harness.NoticeError, Reason: "refused", Text: text}}}, nil)
}
