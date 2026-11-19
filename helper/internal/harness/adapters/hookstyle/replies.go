package hookstyle

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// skipReplies starts reading the harness's log at its end when a turn
// begins in a session not read yet, so an earlier conversation's messages
// stay out of this turn.
func (m *Mapper) skipReplies() {
	session := m.state["harness_session"]
	if m.opts.Replies == nil || session == "" || session == m.replySession {
		return
	}
	_, end := m.readReplies(session, -1)
	m.replySession, m.replyFrom = session, end
}

// replies returns the agent's messages written since the last read, as
// the turn's assistant items. It reads again, for up to wait, until one of
// them is the message want looks for.
func (m *Mapper) replies(at time.Time, draw bool, wait time.Duration, want func(Reply) bool) []harness.Item {
	if m.opts.Replies == nil || m.turn == "" {
		return nil
	}
	session := m.state["harness_session"]
	if session == "" {
		return nil
	}
	if session != m.replySession {
		// A session no prompt began here (the helper restarted mid-turn):
		// its log holds earlier conversation, so read from the end.
		m.seenCalls = map[string]bool{}
		_, end := m.readReplies(session, -1)
		m.replySession, m.replyFrom = session, end
	}
	var got []Reply
	if m.upTo != nil {
		wait = 0
	}
	deadline := time.Now().Add(wait)
	for {
		more, next := m.readReplies(session, m.replyFrom)
		m.replyFrom = next
		got = append(got, more...)
		for _, r := range more {
			for _, c := range r.Calls {
				m.seenCalls[c] = true
			}
		}
		if slices.ContainsFunc(got, want) || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(replyPoll)
	}
	if !draw {
		return nil
	}
	var items []harness.Item
	for _, r := range got {
		t := strings.TrimSpace(r.Text)
		if t == "" {
			continue
		}
		m.n++
		items = append(items, harness.Item{ID: m.turn + ":reply-" + strconv.Itoa(m.n), TurnID: m.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: Clip(t)}})
	}
	return items
}
