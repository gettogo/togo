package engine

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// needsState links question items to the approval that is the same prompt
// at the terminal, so the chat draws one card, and remembers whether a
// hook-fed chat was told it needs its hooks installed.
type needsState struct {
	open    map[string]harness.QuestionBody // unanswered question items by id
	request map[string]string               // question item id to its request id
	relink  []string                        // approval items to republish with their owner
	hint    bool                            // the setup-hooks notice is showing
}

// questionLocked decorates a question item before it is stored: whether
// the phone can answer it, and the request it is linked to.
func (s *sess) questionLocked(it *harness.Item) {
	n := &s.needs
	if n.open == nil {
		n.open, n.request = map[string]harness.QuestionBody{}, map[string]string{}
	}
	if _, ok := s.ad.Actions[harness.ActionAnswer]; !ok {
		f := false
		it.Question.Answerable = &f
	}
	if len(it.Question.Answered) > 0 {
		delete(n.open, it.ID)
	} else {
		n.open[it.ID] = *it.Question
	}
	if req := n.request[it.ID]; req != "" {
		it.Question.RequestID = req
		return
	}
	if len(it.Question.Answered) > 0 || s.e.cfg.Approvals == nil {
		return
	}
	for _, a := range s.e.cfg.Approvals.Pending(s.id) {
		if sameQuestion(*it.Question, a) {
			n.request[it.ID] = a.RequestID
			it.Question.RequestID = a.RequestID
			n.relink = append(n.relink, a.RequestID)
			return
		}
	}
}

// ownerLocked names the open question item a new approval is, and links
// the question to it.
func (s *sess) ownerLocked(a wire.Approval) string {
	n := &s.needs
	for id, q := range n.open {
		if !sameQuestion(q, a) {
			continue
		}
		if n.request[id] == a.RequestID {
			return id
		}
		n.request[id] = a.RequestID
		if it, ok := s.e.log.Get(s.id, id); ok && it.Question != nil {
			body := *it.Question
			body.RequestID = a.RequestID
			it.Question = &body
			s.publishLocked([]harness.Item{it}, nil)
		}
		return id
	}
	return ""
}

// relinkLocked republishes approval items whose question arrived after
// them, now carrying their owner.
func (s *sess) relinkLocked() {
	n := &s.needs
	if len(n.relink) == 0 {
		return
	}
	reqs := n.relink
	n.relink = nil
	owners := map[string]string{}
	for id, req := range n.request {
		owners[req] = id
	}
	var items []harness.Item
	for _, req := range reqs {
		it, ok := s.e.log.Get(s.id, "approval-"+req)
		if !ok || it.Approval == nil || it.Approval.Owner != "" {
			continue
		}
		body := *it.Approval
		body.Owner = owners[req]
		it.Approval = &body
		items = append(items, it)
	}
	if len(items) > 0 {
		s.publishLocked(items, nil)
	}
}

// sameQuestion reports whether request a is one of q's questions at the
// terminal: it shows the question's prompt, or offers all its options.
func sameQuestion(q harness.QuestionBody, a wire.Approval) bool {
	for _, qu := range q.Questions {
		if qu.Prompt != "" && (a.Title == qu.Prompt || strings.Contains(a.Snippet, qu.Prompt)) {
			return true
		}
		if len(qu.Options) > 0 && len(a.Options) > 0 && offersAll(a, qu.Options) {
			return true
		}
	}
	return false
}

// hookHintAfter is how long a chat fed only by hooks waits for its first
// hook before it says the hooks are missing.
var hookHintAfter = 20 * time.Second

// hookHintID is the notice the app links to its integration setup.
const hookHintID = "setup-hooks"

// watchHookFeeds shows the setup-hooks notice when the chat's only feeds
// are agent hooks and none has fired a while after the session started.
func (s *sess) watchHookFeeds() {
	if !s.hookOnlyChat() {
		return
	}
	time.AfterFunc(hookHintAfter, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ctx.Err() != nil || s.hookFiredLocked() || s.needs.hint {
			return
		}
		s.needs.hint = true
		s.publishLocked([]harness.Item{s.hookNotice("warning",
			"The chat needs "+s.ad.Name+"'s hooks, which never fired. Set them up from Togo, or run togo hooks install.")}, nil)
	})
}

// hookFiredLocked clears the notice once a hook arrived.
func (s *sess) hookFiredLocked() bool {
	for _, name := range s.ad.feedOrder {
		if s.ad.Feeds[name].Hook != nil && s.last[name] != nil {
			if s.needs.hint {
				s.needs.hint = false
				s.publishLocked([]harness.Item{s.hookNotice("info", "Hooks connected.")}, nil)
			}
			return true
		}
	}
	return false
}

func (s *sess) hookNotice(level, text string) harness.Item {
	return harness.Item{ID: hookHintID, Kind: harness.KindNotice, At: s.e.cfg.Now(),
		Notice: &harness.NoticeBody{Level: level, Code: harness.NoticeFallback, Text: text}}
}

// hookOnlyChat reports whether the adapter shows a chat whose every feed
// is an agent hook.
func (s *sess) hookOnlyChat() bool {
	if !harness.Has(s.ad.Capabilities, harness.CapChat) || len(s.ad.feedOrder) == 0 {
		return false
	}
	for _, name := range s.ad.feedOrder {
		if f := s.ad.Feeds[name]; f.Hook == nil && f.Statusline == nil {
			return false
		}
	}
	return true
}
