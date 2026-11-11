package engine

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// asker opens approvals no pane shows, answered on a channel, as `togo ask`
// does, and closes them when the question is answered elsewhere. The
// approvals store implements it.
type asker interface {
	OpenAsk(ctx context.Context, p wire.AskParams) (wire.Approval, <-chan string)
	CloseDesk(requestID string)
}

// offerGrace is how long an open question waits for the pane watcher to
// find its dialog on screen before the engine offers it as an ask of its
// own. A prompt the watcher finds is the real dialog, answered with keys,
// and the one push comes from it; a question whose dialog the watcher
// cannot read (or an agent answered through its API) gets the engine's.
var offerGrace = 2 * time.Second

// maxAskOptions bounds the choices a notification offers; a longer list
// is answered in the app.
const maxAskOptions = 6

// offerLocked offers an open question as an approval, so the session is
// needs-you wherever the phone looks: the roster's status and its push
// come from pending approvals, and the Lock Screen and the Watch answer
// one. The approval's title is the question's prompt, which links it to
// the question (ownerLocked): the app draws the question's card, with
// every option, and not a second one. A permission (Approve and Deny set)
// is offered with those two; one single-choice question with a short list
// is offered with its options, and a label picked from a notification
// answers the question through the adapter; anything else (a batch, a
// multi-select, a text answer, a question the phone cannot answer) is
// offered with no option, which the phone shows as "open Togo". Answered
// anywhere, the offered approval closes.
func (s *sess) offerLocked(it harness.Item) {
	q := it.Question
	if q == nil || len(q.Questions) == 0 {
		return
	}
	ask, ok := s.e.cfg.Approvals.(asker)
	if !ok {
		return
	}
	n := &s.needs
	if n.asked == nil {
		n.asked = map[string]string{}
	}
	req, offered := n.asked[it.ID]
	if len(q.Answered) > 0 {
		if offered {
			delete(n.asked, it.ID)
			if req != "" {
				go ask.CloseDesk(req)
			}
		}
		return
	}
	// Already offered, or already a prompt the watcher found on screen.
	if offered || n.request[it.ID] != "" {
		return
	}
	n.asked[it.ID] = ""
	offer := askOffer(it)
	time.AfterFunc(s.e.scaled(offerGrace), func() { s.e.offerAfterGrace(s, offer, ask) })
}

// withdrawOfferLocked drops a question's offered ask: one not opened yet
// stays closed (offerAfterGrace finds no reservation), one open closes.
func (s *sess) withdrawOfferLocked(id string) {
	req, offered := s.needs.asked[id]
	if !offered {
		return
	}
	delete(s.needs.asked, id)
	if req != "" {
		if ask, ok := s.e.cfg.Approvals.(asker); ok {
			go ask.CloseDesk(req)
		}
	}
}

type permissionOffer struct {
	item, question, prompt string
	options                []string          // labels offered, none for "open Togo"
	approve, deny          string            // labels of a permission's approve and deny
	plain                  bool              // choices, not a yes or no: no option gets a role
	choices                map[string]string // label to option id
}

// askOffer builds the ask for a question item: a permission's two labels,
// a short single-choice list, or nothing to pick.
func askOffer(it harness.Item) permissionOffer {
	q := it.Question
	qu := q.Questions[0]
	prompt := qu.Prompt
	if prompt == "" {
		prompt = qu.Header
	}
	o := permissionOffer{item: it.ID, question: qu.ID, prompt: prompt, choices: map[string]string{}}
	if q.Answerable != nil && !*q.Answerable || len(q.Questions) != 1 || qu.Multi {
		return o
	}
	if q.Approve != "" && q.Deny != "" {
		approve, deny := choiceLabel(qu, q.Approve), choiceLabel(qu, q.Deny)
		if approve != "" && deny != "" && approve != deny {
			o.options, o.approve, o.deny = []string{approve, deny}, approve, deny
			o.choices[approve], o.choices[deny] = q.Approve, q.Deny
			return o
		}
	}
	if len(qu.Options) == 0 || len(qu.Options) > maxAskOptions {
		return o
	}
	for _, c := range qu.Options {
		if c.Label == "" {
			return permissionOffer{item: o.item, question: o.question, prompt: o.prompt, choices: map[string]string{}}
		}
		if _, dup := o.choices[c.Label]; dup {
			return permissionOffer{item: o.item, question: o.question, prompt: o.prompt, choices: map[string]string{}}
		}
		o.choices[c.Label] = c.ID
		o.options = append(o.options, c.Label)
	}
	// Only a question that really is a yes or a no is offered as one:
	// exactly two options whose words are an approve and a deny. Any other
	// list is choices, with no role, so no surface draws a Yes that would
	// pick one of them.
	if roles := approval.AskRoles(o.options, "", ""); len(o.options) == 2 && slices.Contains(roles, wire.RoleApprove) && slices.Contains(roles, wire.RoleDeny) {
		for i, r := range roles {
			switch r {
			case wire.RoleApprove:
				o.approve = o.options[i]
			case wire.RoleDeny:
				o.deny = o.options[i]
			}
		}
		return o
	}
	o.plain = true
	return o
}

// offerAfterGrace opens the offered ask once the grace passed, unless the
// question was answered, a prompt found on screen took it meanwhile, or
// the ask would offer nothing to pick while a prompt the watcher found is
// already pending: that prompt is the richer request (Goose's confirm list
// beside its record of the question), and an empty one next to it would
// only be answered by mistake.
func (e *Engine) offerAfterGrace(s *sess, o permissionOffer, ask asker) {
	s.mu.Lock()
	_, still := s.needs.asked[o.item]
	_, open := s.needs.open[o.item]
	if !still || !open || s.needs.request[o.item] != "" || s.ctx.Err() != nil || len(o.options) == 0 && s.screenPromptPendingLocked() {
		if still {
			delete(s.needs.asked, o.item)
		}
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	e.offerPermission(s, o, ask)
}

// screenPromptPendingLocked reports a pending approval the watcher or a
// hook raised for the session: a real prompt, with what it shows.
func (s *sess) screenPromptPendingLocked() bool {
	if s.e.cfg.Approvals == nil {
		return false
	}
	for _, a := range s.e.cfg.Approvals.Pending(s.id) {
		if a.Source != wire.SourceAsk {
			return true
		}
	}
	return false
}

// screenPromptOpenedLocked withdraws the offered asks that have nothing to
// pick: a prompt the watcher found now stands for the session's needs.
func (s *sess) screenPromptOpenedLocked() {
	for id := range s.needs.asked {
		q, open := s.needs.open[id]
		if !open || s.needs.request[id] != "" {
			continue
		}
		if o := askOffer(harness.Item{ID: id, Question: &q}); len(o.options) == 0 {
			s.withdrawOfferLocked(id)
		}
	}
}

// reofferOpenLocked offers again every open question that has no offer
// and no prompt on screen: the prompt that stood for it closed.
func (s *sess) reofferOpenLocked() {
	for id := range s.needs.open {
		if _, offered := s.needs.asked[id]; offered || s.needs.request[id] != "" {
			continue
		}
		if it, ok := s.e.log.Get(s.id, id); ok && it.Question != nil {
			s.offerLocked(it)
		}
	}
}

// offerPermission opens the approval, then waits for its answer. An answer
// from the phone answers the question through the adapter; a close any
// other way (answered in the app or at the terminal, expired, the session
// ended) does nothing.
func (e *Engine) offerPermission(s *sess, o permissionOffer, ask asker) {
	ctx := context.Background()
	id := s.id
	a, answers := ask.OpenAsk(ctx, wire.AskParams{
		SessionID: &id, Title: o.prompt, Options: o.options, Approve: o.approve, Deny: o.deny, Plain: o.plain,
	})
	s.mu.Lock()
	_, still := s.needs.asked[o.item]
	if still {
		s.needs.asked[o.item] = a.RequestID
	}
	s.mu.Unlock()
	if !still {
		// Answered while the approval was opening.
		ask.CloseDesk(a.RequestID)
		return
	}
	label, ok := <-answers
	if !ok {
		return
	}
	choice, known := o.choices[label]
	if !known {
		if o.deny == "" {
			return
		}
		choice = o.choices[o.deny]
	}
	raw, err := json.Marshal(map[string][]string{o.question: {choice}})
	if err != nil {
		return
	}
	act := e.act
	if act == nil {
		act = e.Act
	}
	res, err := act(ctx, harness.ActionRequest{
		Session: id, Action: harness.ActionAnswer, Target: o.item, Args: map[string]json.RawMessage{"answers": raw},
	})
	if err != nil || res.Result == harness.ResultFailed {
		e.cfg.Logf("harness: %s: a question answered from the phone was not applied: %v %s", id, err, res.Message)
	}
}

// reofferLocked offers a still-open question again after its offered ask
// expired: the agent still waits, so the session stays needs-you.
func (s *sess) reofferLocked(owner, reqID string) {
	n := &s.needs
	if n.asked[owner] != reqID {
		return
	}
	delete(n.asked, owner)
	if n.request[owner] == reqID {
		delete(n.request, owner)
	}
	if _, open := n.open[owner]; !open {
		return
	}
	it, ok := s.e.log.Get(s.id, owner)
	if !ok || it.Question == nil {
		return
	}
	body := *it.Question
	body.RequestID = ""
	it.Question = &body
	s.publishLocked([]harness.Item{it}, nil)
}

// offersLocked reports whether approval a is the one offered for question
// id: an ask titled with its prompt, cut as approval titles are.
func (s *sess) offersLocked(id string, q harness.QuestionBody, a wire.Approval) bool {
	if _, offered := s.needs.asked[id]; !offered || a.Source != wire.SourceAsk || len(q.Questions) == 0 {
		return false
	}
	prompt := []rune(askOffer(harness.Item{ID: id, Question: &q}).prompt)
	if len(prompt) > wire.MaxTitle {
		prompt = prompt[:wire.MaxTitle]
	}
	return a.Title == string(prompt)
}

// choiceLabel is the label of q's option id, or "" when q has none.
func choiceLabel(q harness.Question, id string) string {
	for _, c := range q.Options {
		if c.ID == id {
			return c.Label
		}
	}
	return ""
}
