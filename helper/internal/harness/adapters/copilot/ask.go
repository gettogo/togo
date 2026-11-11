package copilot

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Copilot CLI 1.0.108 asks through two tools, both seen live with hooks:
//
// ask_user draws a form: {message, requestedSchema: {properties: {<key>:
// {type, title?, description?, enum? | oneOf[{const, title}]?, items?:
// {enum? | anyOf[{const, title}]?}, default?}}}}, one field per property
// in key order (a tab strip when there are several). A string enum or
// oneOf is a single choice, an array a multi-select (space toggles), a
// boolean Yes / No, a plain string, number or integer a typed answer. Every
// choice field also offers "Other (type your answer)". The PostToolUse
// textResultForLlm is "User responded: <value>" for one field and
// "User responded: k1=v1, k2=v2" for several (array values joined by ", ",
// a oneOf as its const, a boolean as true / false); a decline (ctrl+d) is
// "The user is not available to respond …", a cancel (esc) "User cancelled
// the request.".
//
// exit_plan_mode ({summary, recommendedAction}) draws "Plan Ready for
// Review" with four numbered actions; its result says which one ran.

// The ask tools' names.
const (
	toolAskUser  = "ask_user"
	toolExitPlan = "exit_plan_mode"
)

// The labels Copilot draws for a boolean field.
const (
	labelYes = "Yes"
	labelNo  = "No"
)

// The plan review's actions, as drawn (the label before any parenthesis).
var planActions = []harness.Choice{
	{ID: "Accept plan and continue in Autopilot execution mode"},
	{ID: "Accept plan and continue in Interactive execution mode",
		Detail: "Manual Approval for this session; future defaults unchanged"},
	{ID: "Exit to Interactive execution mode without running the plan",
		Detail: "Manual Approval for this session; future defaults unchanged"},
	{ID: "Suggest changes"},
}

// askField is one property of ask_user's requestedSchema.
type askField struct {
	key     string
	Type    string       `json:"type"`
	Title   string       `json:"title"`
	Desc    string       `json:"description"`
	Enum    []any        `json:"enum"`
	RawOne  []constTitle `json:"oneOf"`
	RawAny  []constTitle `json:"anyOf"`
	Items   *askItems    `json:"items"`
	choices []choice
	multi   bool
}

type askItems struct {
	Enum  []any        `json:"enum"`
	AnyOf []constTitle `json:"anyOf"`
	OneOf []constTitle `json:"oneOf"`
}

type constTitle struct {
	Const any    `json:"const"`
	Title string `json:"title"`
}

// choice is one option: the value Copilot returns and the label it draws.
type choice struct{ value, label string }

type askInput struct {
	Message string          `json:"message"`
	Schema  json.RawMessage `json:"requestedSchema"`
}

// askFields reads requestedSchema.properties in key order, which is the
// order the form draws them.
func askFields(schema json.RawMessage) []askField {
	var s struct {
		Properties json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(s.Properties))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []askField
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := t.(string)
		var f askField
		if dec.Decode(&f) != nil {
			return nil
		}
		f.key = key
		f.choices, f.multi = fieldChoices(f)
		out = append(out, f)
	}
	return out
}

func fieldChoices(f askField) ([]choice, bool) {
	fromEnum := func(vs []any) []choice {
		var cs []choice
		for _, v := range vs {
			s := scalar(v)
			cs = append(cs, choice{s, s})
		}
		return cs
	}
	fromConst := func(vs []constTitle) []choice {
		var cs []choice
		for _, v := range vs {
			s := scalar(v.Const)
			cs = append(cs, choice{s, firstOf(v.Title, s)})
		}
		return cs
	}
	switch f.Type {
	case "array":
		if f.Items == nil {
			return nil, true
		}
		if len(f.Items.Enum) > 0 {
			return fromEnum(f.Items.Enum), true
		}
		return fromConst(append(f.Items.AnyOf, f.Items.OneOf...)), true
	case "boolean":
		return []choice{{"true", labelYes}, {"false", labelNo}}, false
	}
	if len(f.Enum) > 0 {
		return fromEnum(f.Enum), false
	}
	return fromConst(append(f.RawOne, f.RawAny...)), false
}

// askQuestion reads an ask tool's PreToolUse input into questions, one per
// form field (id the field's key) or one for the plan review. An option's
// id is the label drawn on screen, which the key driver selects. ok is
// false for any other tool or an input it cannot read.
func askQuestion(tool string, input json.RawMessage) (*harness.QuestionBody, bool) {
	switch tool {
	case toolAskUser:
	case toolExitPlan:
		return planQuestion(input)
	default:
		return nil, false
	}
	var in askInput
	if json.Unmarshal(input, &in) != nil {
		return nil, false
	}
	fields := askFields(in.Schema)
	if len(fields) == 0 {
		return nil, false
	}
	msg := strings.TrimSpace(in.Message)
	var qs []harness.Question
	for i, f := range fields {
		title := firstOf(f.Title, f.key)
		prompt := title
		if f.Desc != "" {
			prompt += "\n" + f.Desc
		}
		if len(fields) == 1 && msg != "" {
			prompt = msg
			if f.Desc != "" {
				prompt += "\n" + f.Desc
			}
		} else if i == 0 && msg != "" {
			// The screen shows the field's label as the question (the
			// screen_answer driver matches it); the message follows.
			prompt += "\n\n" + msg
		}
		q := harness.Question{ID: f.key, Header: title, Prompt: prompt, Multi: f.multi,
			// Every choice field has an "Other (type your answer)" row; a
			// field without choices is typed.
			AllowText: true}
		for _, c := range f.choices {
			q.Options = append(q.Options, harness.Choice{ID: c.label, Label: c.label})
		}
		qs = append(qs, q)
	}
	return &harness.QuestionBody{Questions: qs}, true
}

// planText is the summary an exit_plan_mode call carries, drawn as the
// plan before its question (hookstyle Options.Plan).
func planText(tool string, input json.RawMessage) (string, bool) {
	if tool != toolExitPlan {
		return "", false
	}
	var in struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal(input, &in) != nil {
		return "", false
	}
	s := strings.TrimSpace(in.Summary)
	return s, s != ""
}

func planQuestion(input json.RawMessage) (*harness.QuestionBody, bool) {
	var in struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal(input, &in) != nil {
		return nil, false
	}
	prompt := "Plan Ready for Review"
	opts := make([]harness.Choice, len(planActions))
	for i, c := range planActions {
		opts[i] = harness.Choice{ID: c.ID, Label: c.ID, Detail: c.Detail}
	}
	return &harness.QuestionBody{
		Questions: []harness.Question{{ID: "0", Header: "Plan", Prompt: prompt, Options: opts}},
		// From the Lock Screen: run with approvals, or leave plan mode
		// without running; autopilot stays an in-app choice.
		Approve: planActions[1].ID, Deny: planActions[2].ID,
	}, true
}

// askAnswered reads an ask tool's PostToolUse response into answers per
// question id: option ids (labels), or the typed text. A decline or cancel
// answers every question with none; nil when the response is unreadable.
func askAnswered(tool string, input, response json.RawMessage) map[string][]string {
	text, ok := resultText(response)
	if !ok {
		return nil
	}
	switch tool {
	case toolAskUser:
	case toolExitPlan:
		return planAnswered(text)
	default:
		return nil
	}
	var in askInput
	if json.Unmarshal(input, &in) != nil {
		return nil
	}
	fields := askFields(in.Schema)
	if len(fields) == 0 {
		return nil
	}
	const said = "User responded: "
	if !strings.HasPrefix(text, said) {
		// Declined, cancelled, or a shape not seen yet: closed, none chosen.
		return none(fields)
	}
	body := strings.TrimSpace(strings.TrimPrefix(text, said))
	values := map[string]string{}
	if len(fields) == 1 {
		values[fields[0].key] = body
	} else {
		values = splitFields(body, fields)
		if values == nil {
			return none(fields)
		}
	}
	out := map[string][]string{}
	for _, f := range fields {
		v, ok := values[f.key]
		if !ok {
			out[f.key] = []string{}
			continue
		}
		out[f.key] = fieldAnswer(f, v)
	}
	return out
}

// splitFields cuts "k1=v1, k2=v2" at each next field's ", key=", keys in
// form order; a field the user left out is missing.
func splitFields(body string, fields []askField) map[string]string {
	out := map[string]string{}
	rest := body
	for i, f := range fields {
		if !strings.HasPrefix(rest, f.key+"=") {
			continue
		}
		rest = rest[len(f.key)+1:]
		end := len(rest)
		for _, next := range fields[i+1:] {
			if j := strings.Index(rest, ", "+next.key+"="); j >= 0 && j < end {
				end = j
			}
		}
		out[f.key] = rest[:end]
		rest = strings.TrimPrefix(rest[end:], ", ")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fieldAnswer maps one field's returned value to option labels, or keeps
// it as typed text.
func fieldAnswer(f askField, v string) []string {
	label := func(s string) (string, bool) {
		for _, c := range f.choices {
			if c.value == s {
				return c.label, true
			}
		}
		return "", false
	}
	if l, ok := label(v); ok {
		return []string{l}
	}
	if !f.multi {
		return []string{v}
	}
	// Array values are joined by ", "; a value may itself hold ", ", so
	// take the longest known value at each step, else text up to the next
	// separator.
	var out []string
	rest := v
	for rest != "" {
		best := ""
		for _, c := range f.choices {
			if (rest == c.value || strings.HasPrefix(rest, c.value+", ")) && len(c.value) > len(best) {
				best = c.value
			}
		}
		part := best
		if part == "" {
			part, _, _ = strings.Cut(rest, ", ")
		}
		if l, ok := label(part); ok {
			out = append(out, l)
		} else {
			out = append(out, part)
		}
		rest = strings.TrimPrefix(rest[len(part):], ", ")
	}
	return out
}

// planAnswered reads which plan action ran. Seen live: "Plan approved!
// Exited plan mode. … interactive mode without automatic execution" for
// the exit-only action; the others are read from their wording.
func planAnswered(text string) map[string][]string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "without automatic execution"):
		return map[string][]string{"0": {planActions[2].ID}}
	case strings.Contains(t, "plan approved") && strings.Contains(t, "autopilot"):
		return map[string][]string{"0": {planActions[0].ID}}
	case strings.Contains(t, "plan approved"):
		return map[string][]string{"0": {planActions[1].ID}}
	}
	return map[string][]string{"0": {}}
}

func none(fields []askField) map[string][]string {
	out := map[string][]string{}
	for _, f := range fields {
		out[f.key] = []string{}
	}
	return out
}

// resultText is a PostToolUse response's text: Copilot's toolResult
// {resultType, textResultForLlm}, or a plain string.
func resultText(response json.RawMessage) (string, bool) {
	var r struct {
		Text string `json:"textResultForLlm"`
	}
	if json.Unmarshal(response, &r) == nil && r.Text != "" {
		return r.Text, true
	}
	var s string
	if json.Unmarshal(response, &s) == nil && s != "" {
		return s, true
	}
	return "", false
}

// scalar is a JSON enum value as Copilot prints it.
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
