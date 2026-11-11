package cursor

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Cursor CLI 2026.11.05's AskQuestion tool (seen live in plan mode with
// composer-2.5; the agent offers it only in plan mode). It fires no
// preToolUse or postToolUse hook, so the hooks never see it; the agent
// transcript (~/.cursor/projects/<slug>/agent-transcripts/<chat>/<chat>.jsonl,
// named by postToolUse's transcript_path) records the call once it is
// answered or skipped, as an assistant tool_use without an id:
//
//	{"type":"tool_use","name":"AskQuestion","input":{"title":"…","questions":
//	  [{"id":"tone","prompt":"…","options":[{"id":"formal","label":"Formal"}],
//	  "allow_multiple":true}]}}
//
// and records no answer. The screen is the live source: a box titled with
// the call's title, "Question N of M", "N. <prompt>", "(multi-select)" for
// allow_multiple, rows "› [ ] Label" / "[x] Label", an "Other: (type to
// answer)" row, footer "↑/↓ option · ←/→ question · Space select · Enter
// next/submit · Esc to skip". Keys: ↓/↑ to a row, Space checks it (typing
// on the Other row fills and checks it), Enter goes to the next question and
// submits on the last, ←/→ switch questions, Esc skips the whole call.
const askTool = "AskQuestion"

type askInput struct {
	Title     string `json:"title"`
	Questions []struct {
		ID      string `json:"id"`
		Prompt  string `json:"prompt"`
		Options []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"options"`
		AllowMultiple  bool `json:"allow_multiple"`
		AllowMultiple2 bool `json:"allowMultiple"`
	} `json:"questions"`
}

// askQuestion reads an AskQuestion call into a question item body. The
// question ids are Cursor's own; option ids are the on-screen labels (the
// answer is typed as keys); every question allows text (the Other row);
// the call's title is the first question's header.
func askQuestion(tool string, input json.RawMessage) (*harness.QuestionBody, bool) {
	if tool != askTool {
		return nil, false
	}
	var in askInput
	if json.Unmarshal(input, &in) != nil || len(in.Questions) == 0 {
		return nil, false
	}
	var qs []harness.Question
	for i, q := range in.Questions {
		id := strings.TrimSpace(q.ID)
		if id == "" {
			id = strconv.Itoa(i)
		}
		x := harness.Question{ID: id, Prompt: strings.TrimSpace(q.Prompt), Multi: q.AllowMultiple || q.AllowMultiple2, AllowText: true}
		for _, o := range q.Options {
			l := strings.TrimSpace(o.Label)
			x.Options = append(x.Options, harness.Choice{ID: l, Label: l})
		}
		qs = append(qs, x)
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		qs[0].Header = t
	}
	return &harness.QuestionBody{Questions: qs}, true
}
