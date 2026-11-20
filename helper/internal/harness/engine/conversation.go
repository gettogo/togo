package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// StateConversation is the mapper state key naming the conversation the
// agent's terminal shows (harness.StateConversation). When it changes, the
// chat starts over on the new conversation: a new log epoch, a fresh mapper
// and a fresh mission, so nothing of the old conversation stays in the chat
// and nothing of the old turn's state (tools, plan, usage, working, held
// prompts) carries into the new one.
const StateConversation = harness.StateConversation

// StateCwd is the mapper state key naming the folder the conversation
// works in now. A mapper sets it when its harness can move a running
// conversation to another folder (OpenCode's session.moved). It is not
// "cwd", which mappers set for other ends (a shell's folder after cd).
const StateCwd = "conversation_cwd"

// filesDir is the folder the @ menu lists: the one the conversation was
// moved to when a mapper names it, else the folder the session's agent is
// in now, else the one the session started in. An agent can change folder
// under a running session (omp's /move, seen on 18.20: the menu kept
// listing the folder the session started in); the session's own record
// follows the pane, so it is asked again each time.
//
// The mapper's word is a path the harness sent, so it is confined as a
// source's path is (the home, the session folder, the agent's own roots).
// A folder that is not one, or cannot be read, is not listed.
func (s *sess) filesDir(ctx context.Context) string {
	isDir := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.IsDir()
	}
	s.mu.Lock()
	dir := s.stateLocked()[StateCwd]
	s.mu.Unlock()
	if filepath.IsAbs(dir) {
		if real, err := source.Confine(dir, s.env); err == nil && isDir(real) {
			return real
		}
		return s.env.Cwd
	}
	if s.e.cfg.Sessions != nil {
		if info, err := s.e.cfg.Sessions.Resolve(ctx, s.id); err == nil {
			if now := source.Expand(info.Cwd, s.e.cfg.Home); filepath.IsAbs(now) && isDir(now) {
				return now
			}
		}
	}
	return s.env.Cwd
}

// movedLocked reports whether the mappers now name a conversation other
// than the one the chat shows. The first one named is the one the session
// started on, unless the session was started again under a running chat
// (a relaunch on a new endpoint, Engine.restart) that showed another: the
// item log is kept across that restart, so it is a move.
func (s *sess) movedLocked() bool {
	id := s.stateLocked()[StateConversation]
	if id == "" || id == s.conv {
		return false
	}
	if s.conv == "" {
		left := s.e.convCarry.take(s.id)
		if left != "" && left != id {
			return true
		}
		s.conv = id
		return false
	}
	return true
}

// followedLocked starts the chat over when a mapper that carries on across
// conversations followed the agent to another one (harness.StateMoves
// rose). The mapper stays: what it learned picks the conversation, and it
// reset its own turn state when it moved. It reports whether the chat
// started over. A count seen before, or a fresh mapper's zero (a helper
// restart), changes nothing.
func (s *sess) followedLocked() bool {
	moves := s.stateLocked()[harness.StateMoves]
	if moves == s.moves {
		return false
	}
	s.moves = moves
	if moves == "" || moves == "0" {
		return false
	}
	s.startOverLocked("")
	if h := s.hookStoreLocked(); h != nil {
		h.remove(false) // the stored chat is the old conversation's
	}
	return true
}

// convCarry keeps, per session, the conversation the chat showed when the
// session's adapter was started again on a new endpoint, until the new
// run's mappers name theirs. It is taken once, and dropped with the
// session (Forget).
type convCarry struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *convCarry) put(id, conv string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if conv == "" {
		delete(c.m, id)
		return
	}
	if c.m == nil {
		c.m = map[string]string{}
	}
	c.m[id] = conv
}

func (c *convCarry) take(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	conv := c.m[id]
	delete(c.m, id)
	return conv
}

// startOverLocked empties the chat for the conversation the agent moved to
// and returns a fresh mapper in place of the one that reported the move,
// given what the old one carries (harness.Carrier); the caller maps the
// announcing record again with it. With no mapper
// named, the mappers stay (followedLocked) and it returns nil. Feeds keep running,
// so no record is missed, and open chats are told to reset and read the
// new epoch. Mappers of other feeds (settings, the status line) describe
// the agent, not the conversation, and stay.
//
// Prompts held for the old conversation are not sent to the new one: a
// notice in the new chat carries their text. A prompt already on its way
// is bound to the conversation it was admitted in by convGen: its sender
// checks the count again before it publishes or records anything.
func (s *sess) startOverLocked(mapper string) harness.Mapper {
	s.forgetSuggestionLocked()
	var held []string
	for _, q := range s.queue {
		if q.item.User != nil {
			held = append(held, q.item.User.Text)
		}
	}
	// An open question of the old conversation is no longer on screen:
	// its offer on the phone is withdrawn, so no answer is typed into a
	// pane that shows another conversation.
	for id := range s.needs.asked {
		s.withdrawOfferLocked(id)
	}
	s.convGen++
	s.e.log.Forget(s.id)
	var fresh harness.Mapper
	if mapper != "" {
		fresh = s.e.mapperFor(mapper, s.mapperEnvLocked())
		if c, ok := s.mappers[mapper].(harness.Carrier); ok {
			c.Carry(fresh)
		}
		s.mappers[mapper] = fresh
	}
	s.conv = ""
	s.queue, s.sent, s.phone = nil, map[string]bool{}, nil
	s.phoneAlias = nil
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

// errMoved is the answer to a prompt that was on its way when the agent
// moved to another conversation: the prompt is not shown as delivered in a
// chat it may not belong to, and it is never sent again.
var errMoved = &harness.FallbackError{Reason: "source", Detail: "The agent moved to another conversation while this was being sent. It may not have arrived; check the terminal before sending it again."}

// errNotSent is the answer to a prompt that was still waiting its turn when
// the agent moved to another conversation: it was written for the old one.
var errNotSent = &harness.FallbackError{Reason: "source", Detail: "The agent moved to another conversation before this was sent, so it was not sent."}

// uncertainNoticeLocked tells the new conversation's chat about a queued
// prompt that was on its way during the move.
func (s *sess) uncertainNoticeLocked(prompt string) {
	text := "A waiting prompt was being sent when " + s.ad.Name + " moved to another conversation. It may not have arrived and was not sent again: " + clipNotice(prompt)
	s.publishLocked([]harness.Item{{ID: "unsent-" + s.e.newID(), Kind: harness.KindNotice, At: s.e.cfg.Now(),
		Notice: &harness.NoticeBody{Level: "error", Code: harness.NoticeError, Reason: "refused", Text: text}}}, nil)
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
