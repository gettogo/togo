package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/harness"
)

// questionHook delivers answers to a blocking AskUserQuestion hook.
type questionHook interface {
	AnswerQuestion(sessionID string, prompts []string, answers map[string]string) bool
}

// askKeyer answers Claude Code's question dialog with keys.
type askKeyer interface {
	AnswerAsk(ctx context.Context, sessionID string, answers []approval.AskAnswer) error
}

// answerClaudeQuestion answers every question of a Claude Code
// AskUserQuestion item. A waiting PermissionRequest hook takes the answers
// as the tool's updatedInput (nothing is typed, line breaks are kept);
// otherwise the dialog is driven with keys, read back step by step.
func (e *Engine) answerClaudeQuestion(ctx context.Context, s *sess, target string, answers map[string][]string) (harness.ActionResult, error) {
	q, res, err := e.openQuestion(s, target)
	if q == nil {
		return res, err
	}
	keyed := make([]approval.AskAnswer, 0, len(q.Questions))
	prompts := make([]string, 0, len(q.Questions))
	texts := map[string]string{}
	for _, qu := range q.Questions {
		vals := nonEmpty(answers[qu.ID])
		if len(vals) == 0 {
			return harness.ActionResult{}, invalidArgs("answer every question")
		}
		a := approval.AskAnswer{Prompt: qu.Prompt, Multi: qu.Multi}
		for _, v := range vals {
			if label, ok := labelOf(qu, v); ok {
				a.Labels = append(a.Labels, label)
			} else if a.Text == "" && (qu.AllowText || len(qu.Options) == 0) {
				a.Text = v
			} else {
				return harness.ActionResult{}, invalidArgs("an answer is not one of the question's options")
			}
		}
		if !qu.Multi && len(a.Labels)+boolInt(a.Text != "") > 1 {
			return harness.ActionResult{}, invalidArgs("a single-choice question takes one answer")
		}
		prompts = append(prompts, qu.Prompt)
		texts[qu.Prompt] = strings.Join(append(a.Labels, nonEmpty([]string{a.Text})...), ", ")
		keyed = append(keyed, a)
	}
	if h, ok := e.cfg.Approvals.(questionHook); ok && h.AnswerQuestion(s.id, prompts, texts) {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	k, ok := e.cfg.Approvals.(askKeyer)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	if err := k.AnswerAsk(ctx, s.id, keyed); err != nil {
		var ae *approval.AskError
		if errors.As(err, &ae) {
			return harness.ActionResult{Result: harness.ResultFailed, Message: ae.Msg}, nil
		}
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The answer could not be sent"}, nil
	}
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

// openQuestion returns the question item target while it can be answered,
// or the result to give instead.
func (e *Engine) openQuestion(s *sess, target string) (*harness.QuestionBody, harness.ActionResult, error) {
	if e.cfg.Approvals == nil {
		return nil, harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	s.mu.Lock()
	it, ok := e.log.Get(s.id, target)
	s.mu.Unlock()
	if !ok || it.Question == nil || len(it.Question.Questions) == 0 {
		return nil, harness.ActionResult{Result: harness.ResultFailed, Message: "The question is no longer open"}, nil
	}
	if len(it.Question.Answered) > 0 {
		return nil, harness.ActionResult{Result: harness.ResultFailed, Message: "The question was already answered"}, nil
	}
	return it.Question, harness.ActionResult{}, nil
}

func labelOf(q harness.Question, id string) (string, bool) {
	for _, o := range q.Options {
		if o.ID == id {
			return o.Label, true
		}
	}
	return "", false
}

func nonEmpty(v []string) []string {
	var out []string
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
