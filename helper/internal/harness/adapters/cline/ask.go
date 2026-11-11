package cline

import (
	"encoding/json"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// askTool is Cline's question tool (3.0.74 `ask_question`, input
// {question, options: [2-5 strings]}). The TUI draws it as "Cline is
// asking a question": the prompt, one row per option, then a
// "Type a response..." row. Keys (read from the bundled TUI): 1-9 pick an
// option at once; ↑/↓ (or Ctrl+P/N) move and Enter picks; any printable key
// moves to the response row and starts typing (a digit only once there),
// Enter sends the text; Esc clears typed text, else cancels. The answer is
// the tool_result: its content is the chosen label or the typed text, and a
// cancel is an error result ("The user did not answer...").
const askTool = "ask_question"

// askQuestionID is the id of an ask's only question.
const askQuestionID = "0"

// askBody is the question an ask_question call shows. Option ids are the
// labels: Cline answers with the label, and the phone answers by keys.
func askBody(input json.RawMessage) *harness.QuestionBody {
	var in struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	_ = json.Unmarshal(input, &in)
	q := harness.Question{ID: askQuestionID, Prompt: strings.TrimSpace(in.Question), AllowText: true}
	for _, o := range in.Options {
		if o = strings.TrimSpace(o); o != "" {
			q.Options = append(q.Options, harness.Choice{ID: o, Label: o})
		}
	}
	return &harness.QuestionBody{Questions: []harness.Question{q}}
}

// askAnswer reads an ask's tool_result: the label or text the user gave,
// or none when the question was cancelled (an error result).
func askAnswer(b block) []string {
	if b.IsError {
		return []string{}
	}
	var s string
	if json.Unmarshal(b.Content, &s) != nil {
		s, _ = resultText(b.Content)
	}
	if s = strings.TrimSpace(s); s == "" {
		return []string{}
	}
	return []string{s}
}

// answered returns item with its question answered.
func answered(item harness.Item, values []string) harness.Item {
	q := *item.Question
	q.Answered = map[string][]string{askQuestionID: values}
	item.Question = &q
	return item
}
