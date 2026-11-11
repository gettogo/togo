package crush

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Crush's question tool (0.100, internal/agent/tools/question.go): one call
// holds up to five questions, each yes_no, single_choice, multi_choice or
// free_text. Its tool_call part carries them as JSON text; the matching
// tool_result's text carries the answers ("Q1: <question>\nUser selected:
// [ids]"), or "User cancelled this question" with is_error. While the
// dialog waits the assistant row has the call and no finish part.
//
// The call becomes a question item, id the call id, instead of a tool row.
// Options are Crush's labels (the dialog is driven with keys, and the rows
// show labels); the ids Crush reports are mapped back to them. Choice
// questions always offer "Something else?", so they take typed words.

const questionTool = "question"

type questionInput struct {
	Questions json.RawMessage `json:"questions"`
}

type questionItem struct {
	Label    string           `json:"label"`
	Type     string           `json:"type"`
	Question string           `json:"question"`
	Choices  []questionChoice `json:"choices"`
	Options  []questionChoice `json:"options"`
}

type questionChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// asked is one open or answered question call: the item, and per question
// the label of each of Crush's choice ids.
type asked struct {
	item  harness.Item
	label []map[string]string
}

// parseQuestions reads the call input; some models send questions as a
// JSON string holding the array, which Crush also accepts.
func parseQuestions(input string) []questionItem {
	var in questionInput
	if json.Unmarshal([]byte(input), &in) != nil || len(in.Questions) == 0 {
		return nil
	}
	var qs []questionItem
	if json.Unmarshal(in.Questions, &qs) != nil {
		var s string
		if json.Unmarshal(in.Questions, &s) != nil || json.Unmarshal([]byte(strings.TrimSpace(s)), &qs) != nil {
			return nil
		}
	}
	return qs
}

// question maps a question call, or returns false when the input holds no
// questions (the call then stays a tool row). A call already answered
// keeps its answers when the row is sent again.
func (m *db) question(callID, input string, at time.Time) (harness.Item, bool) {
	if a, ok := m.asks[callID]; ok {
		return a.item, true
	}
	qs := parseQuestions(input)
	if len(qs) == 0 {
		return harness.Item{}, false
	}
	a := asked{label: make([]map[string]string, len(qs))}
	body := &harness.QuestionBody{}
	for i, q := range qs {
		hq := harness.Question{ID: strconv.Itoa(i), Prompt: q.Question, Options: []harness.Choice{}}
		if len(qs) > 1 {
			hq.Header = q.Label
		}
		a.label[i] = map[string]string{}
		switch q.Type {
		case "yes_no":
			hq.Options = []harness.Choice{{ID: "Yes", Label: "Yes"}, {ID: "No", Label: "No"}}
		case "free_text":
			hq.AllowText = true
		default: // single_choice, multi_choice
			hq.Multi, hq.AllowText = q.Type == "multi_choice", true
			cs := q.Choices
			if len(cs) == 0 {
				cs = q.Options
			}
			for _, c := range cs {
				hq.Options = append(hq.Options, harness.Choice{ID: c.Label, Label: c.Label, Detail: c.Description})
				a.label[i][c.ID] = c.Label
			}
		}
		body.Questions = append(body.Questions, hq)
	}
	a.item = harness.Item{ID: callID, TurnID: m.b.TurnID(), Kind: harness.KindQuestion, At: at, Question: body}
	m.asks[callID] = a
	return a.item, true
}

// answered closes a question call from its tool_result.
func (m *db) answered(callID, content string, isError bool) (harness.Item, bool) {
	a, ok := m.asks[callID]
	if !ok || a.item.Question == nil || a.item.Question.Answered != nil {
		return harness.Item{}, false
	}
	var got map[string][]string
	if !isError {
		got = parseAnswers(content, a.label)
	}
	return m.close(callID, got), true
}

// close settles a question call with the answers given; every question
// left out gets an empty answer, so the card closes.
func (m *db) close(callID string, got map[string][]string) harness.Item {
	a := m.asks[callID]
	qb := *a.item.Question
	qb.Answered = map[string][]string{}
	for _, q := range qb.Questions {
		qb.Answered[q.ID] = []string{}
		if v := got[q.ID]; len(v) > 0 {
			qb.Answered[q.ID] = v
		}
	}
	a.item.Question = &qb
	m.asks[callID] = a
	return a.item
}

// closeOpen closes every question still waiting, when the turn ended
// without its result (cancelled, failed).
func (m *db) closeOpen() []harness.Item {
	var items []harness.Item
	for id, a := range m.asks {
		if a.item.Question != nil && a.item.Question.Answered == nil {
			items = append(items, m.close(id, nil))
		}
	}
	return items
}

var answerHead = regexp.MustCompile(`(?m)^Q(\d+): `)

// parseAnswers reads the tool result: per question a "Q<n>: <question>"
// line, then "User selected: [ids]" and/or "User provided: <text>", or
// "User answered: yes|no", or "User skipped this question"; notes follow
// a "Notes:" line and are not answers.
func parseAnswers(content string, labels []map[string]string) map[string][]string {
	out := map[string][]string{}
	heads := answerHead.FindAllStringSubmatchIndex(content, -1)
	for k, h := range heads {
		n, err := strconv.Atoi(content[h[2]:h[3]])
		if err != nil || n < 1 || n > len(labels) {
			continue
		}
		end := len(content)
		if k+1 < len(heads) {
			end = heads[k+1][0]
		}
		block := content[h[1]:end]
		if i := strings.Index(block, "\n\nNotes:"); i >= 0 {
			block = block[:i]
		}
		_, body, _ := strings.Cut(block, "\n") // the question line
		id := strconv.Itoa(n - 1)
		var vals []string
		for _, line := range strings.Split(body, "\n") {
			switch {
			case strings.HasPrefix(line, "User selected: "):
				var ids []string
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "User selected: ")), &ids) == nil {
					for _, c := range ids {
						if l, ok := labels[n-1][c]; ok {
							c = l
						}
						vals = append(vals, c)
					}
				}
			case strings.HasPrefix(line, "User provided: "):
				// Free text may run over several lines: the rest of the block.
				i := strings.Index(body, "User provided: ")
				vals = append(vals, strings.TrimSpace(body[i+len("User provided: "):]))
			case line == "User answered: yes":
				vals = append(vals, "Yes")
			case line == "User answered: no":
				vals = append(vals, "No")
			}
			if strings.HasPrefix(line, "User provided: ") {
				break
			}
		}
		out[id] = vals
	}
	return out
}
