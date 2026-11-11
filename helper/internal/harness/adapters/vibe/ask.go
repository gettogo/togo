package vibe

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// Vibe 3.1 raises two prompts, both read from its journal (real captures,
// 2026-11-10):
//
//   - ask_user_question: an effect entry whose detail.toolName is
//     "ui.ask_user_question" and whose detail.input is the request
//     ({questions[]{question, header, options[]{label, description},
//     multiSelect, hideOther}}). It settles completed with
//     output.structured_content {answers[]{question, answer, isOther},
//     cancelled}; a multi-select answer is the chosen labels joined by
//     ", ", a typed one has isOther. Esc and Ctrl+C both settle it
//     completed with cancelled true and no answers.
//   - a tool permission: a callback_registered record (not a projection
//     delta) whose routing is a callback entry of kind approval, with the
//     effect it gates and the choices offered; callback_resolved carries
//     the decision. The dialog lists the choices as "1. Allow once",
//     "2. Allow for remainder of this session", "3. Always allow",
//     "4. Deny" ("Esc reject"); a digit picks its row at once.
//
// Both become question items. Option ids are the labels the dialog shows,
// since the phone's answer is typed into the dialog.

const askTool = "ui.ask_user_question"

func isAsk(name string) bool { return name == askTool || name == "ask_user_question" }

// askRequest is UserQuestionRequest, camelCase as journaled (snake_case
// accepted too).
type askRequest struct {
	Questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		Multi      bool `json:"multiSelect"`
		MultiSnake bool `json:"multi_select"`
		Hide       bool `json:"hideOther"`
		HideSnake  bool `json:"hide_other"`
	} `json:"questions"`
}

type askResult struct {
	Answers []struct {
		Question     string `json:"question"`
		Answer       string `json:"answer"`
		IsOther      bool   `json:"isOther"`
		IsOtherSnake bool   `json:"is_other"`
	} `json:"answers"`
	Cancelled bool `json:"cancelled"`
}

func askQuestions(raw json.RawMessage) []harness.Question {
	var req askRequest
	if json.Unmarshal(raw, &req) != nil {
		return nil
	}
	var out []harness.Question
	for i, q := range req.Questions {
		hq := harness.Question{ID: strconv.Itoa(i), Header: q.Header, Prompt: q.Question,
			Multi: q.Multi || q.MultiSnake, AllowText: !(q.Hide || q.HideSnake)}
		for _, o := range q.Options {
			if o.Label == "" {
				continue
			}
			hq.Options = append(hq.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
		}
		out = append(out, hq)
	}
	return out
}

// askEffect maps an ask_user_question effect; ok is false when its input
// holds no question, so it stays a tool row.
func (s *session) askEffect(e entry, at time.Time) (harness.Item, bool) {
	qs := askQuestions(e.Detail.Input)
	if len(qs) == 0 {
		return harness.Item{}, false
	}
	if prev, ok := s.asks[e.ID]; ok {
		at = prev.At
	}
	x := harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
	switch e.State.Status {
	case "", "running", "open":
		s.asks[e.ID] = x
		return x, true
	case "completed":
		var o struct {
			Structured askResult `json:"structured_content"`
		}
		_ = json.Unmarshal(e.State.Output, &o)
		x.Question.Answered = answered(qs, o.Structured)
	default:
		x.Question.Answered = dismissed(qs)
	}
	delete(s.asks, e.ID)
	return x, true
}

// answered turns Vibe's answers (keyed by question text) into option ids
// or typed text per question id. A cancel answers every question with
// nothing.
func answered(qs []harness.Question, r askResult) map[string][]string {
	out := dismissed(qs)
	if r.Cancelled {
		return out
	}
	for i, a := range r.Answers {
		k := -1
		for j, q := range qs {
			if q.Prompt == a.Question {
				k = j
				break
			}
		}
		if k < 0 && i < len(qs) {
			k = i
		}
		if k < 0 {
			continue
		}
		q := qs[k]
		switch {
		case a.Answer == "":
		case a.IsOther || a.IsOtherSnake || !q.Multi:
			out[q.ID] = []string{a.Answer}
		default:
			out[q.ID] = splitLabels(a.Answer, q.Options)
		}
	}
	return out
}

// splitLabels reads a multi-select answer, the labels joined by ", ", a
// label that itself holds ", " included. Text that is no label is kept as
// one typed value.
func splitLabels(ans string, opts []harness.Choice) []string {
	is := map[string]bool{}
	for _, o := range opts {
		is[o.ID] = true
	}
	parts := strings.Split(ans, ", ")
	var out, rest []string
	for i := 0; i < len(parts); {
		j := len(parts)
		for ; j > i; j-- {
			if is[strings.Join(parts[i:j], ", ")] {
				break
			}
		}
		if j == i {
			rest = append(rest, parts[i])
			i++
			continue
		}
		out = append(out, strings.Join(parts[i:j], ", "))
		i = j
	}
	if len(rest) > 0 {
		out = append(out, strings.Join(rest, ", "))
	}
	return out
}

func dismissed(qs []harness.Question) map[string][]string {
	out := make(map[string][]string, len(qs))
	for _, q := range qs {
		out[q.ID] = []string{}
	}
	return out
}

// callbackEntry is a PublicCallbackEntry as callback_registered routes it.
type callbackEntry struct {
	ID        string `json:"id"`
	TurnID    string `json:"turnId"`
	CreatedAt int64  `json:"createdAt"`
	Related   string `json:"relatedEntryId"`
	Detail    struct {
		Kind    string   `json:"kind"`
		Choices []string `json:"choices"`
		Effect  struct {
			ToolName string          `json:"toolName"`
			Input    json.RawMessage `json:"input"`
		} `json:"effect"`
		Permissions []struct {
			Label      string `json:"label"`
			Invocation string `json:"invocationPattern"`
		} `json:"requiredPermissions"`
		Reason string `json:"reason"`
	} `json:"detail"`
}

// approvalLabels are the dialog's rows for ApprovalDecisionType, in the
// order Vibe lists them; cancel_turn has no row (Esc).
var approvalLabels = []struct{ choice, label string }{
	{"approve", "Allow once"},
	{"approve_for_session", "Allow for remainder of this session"},
	{"approve_permanently", "Always allow"},
	{"deny", "Deny"},
}

func approvalLabel(choice string) string {
	for _, l := range approvalLabels {
		if l.choice == choice {
			return l.label
		}
	}
	return ""
}

// callbackOpened maps an approval callback: a question with Approve and
// Deny set, and the tool it gates shown waiting.
func (s *session) callbackOpened(c callbackEntry) []harness.Item {
	if c.ID == "" || c.Detail.Kind != "approval" {
		return nil
	}
	offered := map[string]bool{}
	for _, ch := range c.Detail.Choices {
		offered[ch] = true
	}
	if len(c.Detail.Choices) == 0 {
		for _, l := range approvalLabels {
			offered[l.choice] = true
		}
	}
	q := harness.Question{ID: "0", Prompt: approvalPrompt(c)}
	body := &harness.QuestionBody{}
	for _, l := range approvalLabels {
		if !offered[l.choice] {
			continue
		}
		q.Options = append(q.Options, harness.Choice{ID: l.label, Label: l.label})
		switch l.choice {
		case "approve":
			body.Approve = l.label
		case "deny":
			body.Deny = l.label
		}
	}
	body.Questions = []harness.Question{q}
	at := ms(c.CreatedAt)
	if prev, ok := s.asks[c.ID]; ok {
		at = prev.At
	}
	_, items := s.open(c.TurnID, at)
	x := harness.Item{ID: c.ID, TurnID: c.TurnID, Kind: harness.KindQuestion, At: at, Question: body}
	s.asks[c.ID] = x
	if t, ok := s.tools[c.Related]; ok {
		b := *t.Tool
		b.State = harness.StateWaiting
		t.Tool = &b
		s.tools[c.Related] = t
		items = append(items, t)
	}
	s.gates[c.ID] = c.Related
	return append(items, x)
}

// approvalPrompt is the dialog's own text: "Permission for the
// file_system.bash tool (touch *)", then what it would run or touch.
func approvalPrompt(c callbackEntry) string {
	tool := c.Detail.Effect.ToolName
	if tool == "" {
		tool = "a"
	}
	var labels []string
	for _, p := range c.Detail.Permissions {
		if p.Label != "" {
			labels = append(labels, p.Label)
		}
	}
	lines := []string{"Permission for the " + tool + " tool"}
	if len(labels) > 0 {
		lines[0] += " (" + strings.Join(labels, ", ") + ")"
	}
	var in map[string]any
	_ = json.Unmarshal(c.Detail.Effect.Input, &in)
	for _, k := range []string{"command", "path", "file_path", "url", "query"} {
		if v, ok := in[k].(string); ok && v != "" {
			lines = append(lines, hookstyle.Truncate(v, 400))
			break
		}
	}
	if len(lines) == 1 {
		for _, p := range c.Detail.Permissions {
			if p.Invocation != "" {
				lines = append(lines, hookstyle.Truncate(p.Invocation, 400))
				break
			}
		}
	}
	if r := strings.TrimSpace(c.Detail.Reason); r != "" {
		lines = append(lines, r)
	}
	return strings.Join(lines, "\n")
}

type callbackResult struct {
	Output struct {
		Type     string `json:"type"`
		Decision struct {
			Type string `json:"type"`
		} `json:"decision"`
	} `json:"output"`
}

// callbackResolved closes an approval with the row Vibe recorded; a
// cancelled turn, an expiry or an unknown decision answers nothing.
func (s *session) callbackResolved(id string, r callbackResult) []harness.Item {
	x, ok := s.asks[id]
	if !ok || x.Question == nil {
		return nil
	}
	delete(s.asks, id)
	body := *x.Question
	body.Answered = dismissed(body.Questions)
	if r.Output.Type == "approval" {
		if l := approvalLabel(r.Output.Decision.Type); l != "" {
			body.Answered["0"] = []string{l}
		}
	}
	x.Question = &body
	items := []harness.Item{x}
	if rel := s.gates[id]; rel != "" {
		delete(s.gates, id)
		if t, ok := s.tools[rel]; ok && t.Tool.State == harness.StateWaiting {
			b := *t.Tool
			b.State = harness.StateRunning
			t.Tool = &b
			s.tools[rel] = t
			items = append(items, t)
		}
	}
	return items
}

// closeAsks answers, with nothing, every prompt of a turn that ended
// while it was still open.
func (s *session) closeAsks(turn string) []harness.Item {
	var items []harness.Item
	for id, x := range s.asks {
		if x.TurnID != turn {
			continue
		}
		body := *x.Question
		body.Answered = dismissed(body.Questions)
		x.Question = &body
		items = append(items, x)
		delete(s.asks, id)
		delete(s.gates, id)
	}
	return items
}
