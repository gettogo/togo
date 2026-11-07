package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// answerQuestion answers a question item through the pending approval
// that is that question: the one request whose options carry every option
// of the question (and, when the agent puts it there, its prompt). The
// chosen option is matched by label; typed text goes only to a request
// that takes text. Nothing is ever sent to another request: a missing or
// answered item, no matching request, or two that both match all fail.
func (e *Engine) answerQuestion(ctx context.Context, s *sess, target string, answers map[string][]string) (harness.ActionResult, error) {
	if e.cfg.Approvals == nil {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	s.mu.Lock()
	it, ok := e.log.Get(s.id, target)
	s.mu.Unlock()
	if !ok || it.Question == nil || len(it.Question.Questions) == 0 {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The question is no longer open"}, nil
	}
	if len(it.Question.Answered) > 0 {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The question was already answered"}, nil
	}
	var (
		q      harness.Question
		chosen string
	)
	for _, qu := range it.Question.Questions {
		if v := answers[qu.ID]; len(v) > 0 && v[0] != "" {
			q, chosen = qu, v[0]
			break
		}
	}
	if chosen == "" {
		return harness.ActionResult{}, invalidArgs("answers names no question of this item")
	}
	a, ok := requestFor(e.cfg.Approvals.Pending(s.id), q)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The question is not waiting at the terminal"}, nil
	}
	raw, ok := decisionFor(q, chosen, a)
	if !ok {
		return harness.ActionResult{}, invalidArgs("the answer is not one of the question's options")
	}
	if err := e.cfg.Approvals.Answer(ctx, a.RequestID, raw, wire.ByPhone); err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The answer was not accepted"}, nil
	}
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

// requestFor finds the single pending request that is question q. A
// request fits when it offers every option label of q (a text-only
// question needs a request that takes text and shows its prompt). Among
// fitting requests, those that show q's prompt are preferred; more than
// one left is ambiguous.
func requestFor(pending []wire.Approval, q harness.Question) (wire.Approval, bool) {
	var fit, named []wire.Approval
	for _, a := range pending {
		shows := q.Prompt != "" && (strings.Contains(a.Title, q.Prompt) || strings.Contains(a.Snippet, q.Prompt) ||
			(a.Detail != nil && strings.Contains(*a.Detail, q.Prompt)))
		if len(q.Options) == 0 {
			if a.AllowText && shows {
				fit = append(fit, a)
			}
			continue
		}
		if !offersAll(a, q.Options) {
			continue
		}
		fit = append(fit, a)
		if shows {
			named = append(named, a)
		}
	}
	if len(named) > 0 {
		fit = named
	}
	if len(fit) != 1 {
		return wire.Approval{}, false
	}
	return fit[0], true
}

func offersAll(a wire.Approval, choices []harness.Choice) bool {
	for _, c := range choices {
		if _, ok := optionN(a, c.Label); !ok {
			return false
		}
	}
	return true
}

func optionN(a wire.Approval, label string) (int, bool) {
	for _, o := range a.Options {
		if o.Label == label {
			return o.N, true
		}
	}
	return 0, false
}

// decisionFor maps a chosen option id of q to the request's option by
// label, or to typed text when both the question and the request take it.
func decisionFor(q harness.Question, chosen string, a wire.Approval) (string, bool) {
	for _, o := range q.Options {
		if o.ID == chosen {
			n, ok := optionN(a, o.Label)
			return fmt.Sprintf("option:%d", n), ok
		}
	}
	if (q.AllowText || len(q.Options) == 0) && a.AllowText {
		return "text:" + chosen, true
	}
	return "", false
}
