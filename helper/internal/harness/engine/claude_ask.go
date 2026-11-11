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

// choiceKeyer answers a selection dialog detect.ParseChoice reads (omp's
// ask, clack lists) with keys.
type choiceKeyer interface {
	AnswerChoice(ctx context.Context, sessionID string, answers []approval.AskAnswer) error
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
	if !takesText(q) {
		// Not an AskUserQuestion dialog but a menu Claude draws for a
		// tool (ExitPlanMode's "Would you like to proceed?"): the prompt
		// the watcher found on screen is the question, answered by its
		// keys.
		return e.answerQuestion(ctx, s, target, answers)
	}
	keyed, prompts, texts, err := keyedAnswers(q, answers)
	if err != nil {
		return harness.ActionResult{}, err
	}
	if h, ok := e.cfg.Approvals.(questionHook); ok && h.AnswerQuestion(s.id, prompts, texts) {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	k, ok := e.cfg.Approvals.(askKeyer)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	return keyedResult(k.AnswerAsk(ctx, s.id, keyed))
}

// answerScreenQuestion answers every question of an item whose dialog is
// on screen as a selection list (the screen_answer builtin): the keys move
// the cursor, toggle, type and submit, read back after every step. The
// adapter's record of the answer closes the item, as when the user answers
// at the terminal.
func (e *Engine) answerScreenQuestion(ctx context.Context, s *sess, target string, answers map[string][]string) (harness.ActionResult, error) {
	q, res, err := e.openQuestion(s, target)
	if q == nil {
		return res, err
	}
	keyed, _, _, err := keyedAnswers(q, answers)
	if err != nil {
		return harness.ActionResult{}, err
	}
	k, ok := e.cfg.Approvals.(choiceKeyer)
	if !ok {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	return keyedResult(k.AnswerChoice(ctx, s.id, keyed))
}

// takesText reports an AskUserQuestion item: every question of one takes
// typed words (Claude's "Type something" row).
func takesText(q *harness.QuestionBody) bool {
	for _, qu := range q.Questions {
		if qu.AllowText {
			return true
		}
	}
	return false
}

// keyedAnswers checks an answer action's values against the item's
// questions and turns them into one AskAnswer per question: the labels
// chosen, or typed text where the question takes it. Every question must
// be answered; a single-choice question takes one answer.
func keyedAnswers(q *harness.QuestionBody, answers map[string][]string) (keyed []approval.AskAnswer, prompts []string, texts map[string]string, err error) {
	texts = map[string]string{}
	for _, qu := range q.Questions {
		vals := nonEmpty(answers[qu.ID])
		if len(vals) == 0 {
			return nil, nil, nil, invalidArgs("answer every question")
		}
		a := approval.AskAnswer{Prompt: qu.Prompt, Multi: qu.Multi}
		for _, v := range vals {
			if label, ok := labelOf(qu, v); ok {
				a.Labels = append(a.Labels, label)
			} else if a.Text == "" && (qu.AllowText || len(qu.Options) == 0) {
				a.Text = v
			} else {
				return nil, nil, nil, invalidArgs("an answer is not one of the question's options")
			}
		}
		if !qu.Multi && len(a.Labels)+boolInt(a.Text != "") > 1 {
			return nil, nil, nil, invalidArgs("a single-choice question takes one answer")
		}
		prompts = append(prompts, qu.Prompt)
		texts[qu.Prompt] = strings.Join(append(a.Labels, nonEmpty([]string{a.Text})...), ", ")
		keyed = append(keyed, a)
	}
	return keyed, prompts, texts, nil
}

// keyedResult turns a key driver's outcome into an action result: applied,
// or failed with the driver's own words.
func keyedResult(err error) (harness.ActionResult, error) {
	if err == nil {
		return harness.ActionResult{Result: harness.ResultApplied}, nil
	}
	var ae *approval.AskError
	if errors.As(err, &ae) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: ae.Msg}, nil
	}
	return harness.ActionResult{Result: harness.ResultFailed, Message: "The answer could not be sent"}, nil
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
