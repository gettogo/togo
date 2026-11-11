package claude

import (
	"encoding/json"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// questionResult settles an AskUserQuestion item from its tool result:
// toolUseResult.answers maps each question's text to the chosen label,
// labels joined with ", " for a multi-select, or the text typed. Answers
// are mapped back to the item's option ids (text that names no option
// stays text). A rejected or failed call settles every question with no
// answer, so the card closes instead of staying answerable.
func (t *transcript) questionResult(e entry, b block, q *harness.Item) []harness.Item {
	delete(t.questions, b.ToolUseID)
	it := *q
	body := *q.Question
	body.Answered = map[string][]string{}
	var res struct {
		Answers map[string]string `json:"answers"`
	}
	if !b.IsError {
		_ = json.Unmarshal(e.ToolUseResult, &res)
	}
	for _, qu := range body.Questions {
		body.Answered[qu.ID] = answerIDs(qu, res.Answers[qu.Prompt])
	}
	if body.Approve == "manual" && len(body.Questions) == 1 && body.Questions[0].ID == "plan" && !b.IsError {
		// ExitPlanMode's result says the plan was approved, not which yes.
		body.Answered["plan"] = []string{"Yes"}
	}
	it.Question = &body
	return []harness.Item{it}
}

// answerIDs maps one answer string to option ids: the whole string when it
// is one label, else each ", "-separated part of a multi-select.
func answerIDs(q harness.Question, answer string) []string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return []string{}
	}
	if id, ok := optionID(q, answer); ok {
		return []string{id}
	}
	if !q.Multi {
		return []string{answer}
	}
	var out []string
	for _, part := range strings.Split(answer, ", ") {
		if id, ok := optionID(q, strings.TrimSpace(part)); ok {
			out = append(out, id)
		} else if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func optionID(q harness.Question, label string) (string, bool) {
	for _, o := range q.Options {
		if o.Label == label {
			return o.ID, true
		}
	}
	return "", false
}
