package droid

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Droid 1.5's AskUser tool (read from the 1.5.0 binary, then seen live with
// a custom model, testdata/session-ask-spec.jsonl). Its input is either the
// documented plain-text questionnaire,
//
//  1. [question] Which features do you want to enable? (multi)
//     [topic] Features
//     [option] Auth handling
//     [option] Login Page
//
// or a Claude-shaped {questions: [{question, header, options: [{label,
// description}], multiSelect}]}; both carry `blocking` and `context`. The TUI
// always adds "Or type your own answer...". The result (tool_response, and
// the transcript's tool_result) is text, one block per answered question:
//
//  1. [question] Which features do you want to enable?
//     [answer] Auth handling, Login Page
//
// with a (multi) answer joined by commas. A cancel ends the call with an
// error ("User cancelled AskUser"). Droid's hooks carry no tool call id.
const askTool = "AskUser"

// Questionnaire limits Droid enforces (2-4 options, at most 4 questions).
const (
	askMaxQuestions = 4
	askMaxOptions   = 4
)

var (
	askQuestionLine = regexp.MustCompile(`(?i)^(?:\d+\.\s*)?\[question\][ \t]*(.*)$`)
	askTopicLine    = regexp.MustCompile(`(?i)^\[topic\][ \t]*(.*)$`)
	askOptionLine   = regexp.MustCompile(`(?i)^\[option\][ \t]*(.*)$`)
	askNumbered     = regexp.MustCompile(`^(\d+)\.\s*(.*)$`)
	askMulti        = regexp.MustCompile(`(?i)^(.*?)\s*\(multi\)\s*$`)
	askAnswerHead   = regexp.MustCompile(`(?i)^(\d+)\.\s*\[question\]`)
	askAnswerLine   = regexp.MustCompile(`(?i)^\[answer\][ \t]*(.*)$`)
	askHeading      = regexp.MustCompile(`^#+\s`)
)

type askInput struct {
	Questionnaire *string `json:"questionnaire"`
	Questions     []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		MultiSelect bool `json:"multiSelect"`
	} `json:"questions"`
	Blocking *bool `json:"blocking"`
}

// askQuestion reads an AskUser call into a question item body: ids "0",
// "1", … in order, the topic as the header, option ids the labels (the
// screen's wording), AllowText always (the TUI's own-answer row). A
// non-blocking call only queues the questions for later, so it is not a
// prompt on screen and reports false.
func askQuestion(tool string, input json.RawMessage) (*harness.QuestionBody, bool) {
	if tool != askTool {
		return nil, false
	}
	var in askInput
	if json.Unmarshal(input, &in) != nil {
		return nil, false
	}
	if in.Blocking != nil && !*in.Blocking {
		return nil, false
	}
	var qs []harness.Question
	switch {
	case len(in.Questions) > 0:
		for i, q := range in.Questions {
			x := harness.Question{ID: strconv.Itoa(i), Header: strings.TrimSpace(q.Header),
				Prompt: strings.TrimSpace(q.Question), Multi: q.MultiSelect, AllowText: true}
			for _, o := range q.Options {
				l := strings.TrimSpace(o.Label)
				x.Options = append(x.Options, harness.Choice{ID: l, Label: l, Detail: strings.TrimSpace(o.Description)})
			}
			qs = append(qs, x)
		}
	case in.Questionnaire != nil:
		qs = parseQuestionnaire(*in.Questionnaire)
	}
	if len(qs) == 0 {
		return nil, false
	}
	return &harness.QuestionBody{Questions: qs}, true
}

// parseQuestionnaire follows Droid's own parser: markdown headings and
// fences dropped, text before the first [question] skipped, extra lines
// before a question's first [topic]/[option] continue its text, "(multi)"
// at a question's end allows several answers, a numbered line followed by
// a [topic] or [option] starts a question too, no options means Yes/No,
// and a missing topic is "Q<n>". Droid rejects malformed input before it
// asks; this reader keeps what it can.
func parseQuestionnaire(text string) []harness.Question {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		if strings.HasPrefix(t, "```") || askHeading.MatchString(t) {
			continue
		}
		lines = append(lines, t)
	}
	for i, l := range lines {
		if askQuestionLine.MatchString(l) {
			lines = lines[i:]
			break
		}
	}
	type draft struct {
		q        harness.Question
		explicit bool
		topic    bool
	}
	var out []harness.Question
	var cur *draft
	flush := func() {
		if cur == nil {
			return
		}
		if len(cur.q.Options) == 0 {
			cur.q.Options = []harness.Choice{{ID: "Yes", Label: "Yes"}, {ID: "No", Label: "No"}}
		}
		if cur.q.Header == "" {
			cur.q.Header = "Q" + strconv.Itoa(len(out)+1)
		}
		cur.q.ID = strconv.Itoa(len(out))
		cur.q.AllowText = true
		out = append(out, cur.q)
		cur = nil
	}
	start := func(prompt string, explicit bool) {
		flush()
		q := harness.Question{}
		if m := askMulti.FindStringSubmatch(prompt); m != nil {
			prompt, q.Multi = m[1], true
		}
		q.Prompt = strings.TrimSpace(prompt)
		cur = &draft{q: q, explicit: explicit}
	}
	for i, l := range lines {
		if l == "" {
			continue
		}
		if m := askQuestionLine.FindStringSubmatch(l); m != nil {
			if len(out) >= askMaxQuestions {
				break
			}
			start(m[1], true)
			continue
		}
		if m := askTopicLine.FindStringSubmatch(l); m != nil {
			if cur != nil {
				cur.q.Header = strings.Join(strings.Fields(m[1]), "-")
				cur.topic = true
			}
			continue
		}
		if m := askOptionLine.FindStringSubmatch(l); m != nil {
			if cur != nil && len(cur.q.Options) < askMaxOptions {
				o := strings.TrimSpace(m[1])
				cur.q.Options = append(cur.q.Options, harness.Choice{ID: o, Label: o})
			}
			continue
		}
		if cur != nil && cur.explicit && !cur.topic && len(cur.q.Options) == 0 {
			if m := askMulti.FindStringSubmatch(l); m != nil {
				cur.q.Multi = true
				l = m[1]
			}
			if l != "" {
				cur.q.Prompt += "\n" + l
			}
			continue
		}
		if m := askNumbered.FindStringSubmatch(l); m != nil {
			next := ""
			for _, n := range lines[i+1:] {
				if n != "" {
					next = n
					break
				}
			}
			if askTopicLine.MatchString(next) || askOptionLine.MatchString(next) {
				if len(out) >= askMaxQuestions {
					break
				}
				start(m[2], false)
			}
		}
	}
	flush()
	return out
}

// askAnswered reads the answers from an AskUser result: question id ->
// the chosen labels, or the typed answer. A (multi) answer is split on its
// commas unless it is one label whole. A cancel sets every question to
// none; nil when the result is not readable.
func askAnswered(tool string, input, response json.RawMessage) map[string][]string {
	q, ok := askQuestion(tool, input)
	if !ok {
		return nil
	}
	text := responseText(response)
	if text == "" {
		return nil
	}
	low := strings.ToLower(text)
	if strings.Contains(low, "cancelled askuser") || strings.Contains(low, "askuser cancelled") ||
		strings.Contains(low, "cancelled by user") || strings.Contains(low, "dismissed") {
		out := map[string][]string{}
		for _, x := range q.Questions {
			out[x.ID] = []string{}
		}
		return out
	}
	answers := map[int]string{}
	cur := 0
	var buf []string
	inAnswer := false
	save := func() {
		if cur > 0 && inAnswer {
			answers[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
	}
	for _, l := range strings.Split(text, "\n") {
		if m := askAnswerHead.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			save()
			cur, _ = strconv.Atoi(m[1])
			buf, inAnswer = nil, false
			continue
		}
		if m := askAnswerLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			buf, inAnswer = []string{m[1]}, true
			continue
		}
		if inAnswer {
			buf = append(buf, l)
		}
	}
	save()
	if len(answers) == 0 {
		return nil
	}
	out := map[string][]string{}
	for i, x := range q.Questions {
		a, ok := answers[i+1]
		if !ok || a == "" || a == "(no answer)" {
			out[x.ID] = []string{}
			continue
		}
		out[x.ID] = pickAnswer(x, a)
	}
	return out
}

// pickAnswer maps an answer's text to option ids: one label whole, else
// (multi) each comma-separated part, each a label when it is one.
func pickAnswer(q harness.Question, a string) []string {
	if id, ok := optionID(q, a); ok {
		return []string{id}
	}
	if !q.Multi {
		return []string{a}
	}
	var out []string
	for _, p := range strings.Split(a, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if id, ok := optionID(q, p); ok {
			out = append(out, id)
		} else {
			out = append(out, p)
		}
	}
	return out
}

func optionID(q harness.Question, label string) (string, bool) {
	for _, o := range q.Options {
		if strings.EqualFold(strings.TrimSpace(o.Label), strings.TrimSpace(label)) {
			return o.ID, true
		}
	}
	return "", false
}

// responseText is a tool_response given as a string, or the text inside
// an object or a content list.
func responseText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.Error != "" {
			return o.Error
		}
		if o.Text != "" {
			return o.Text
		}
		if len(o.Content) > 0 {
			return responseText(o.Content)
		}
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b []string
		for _, p := range parts {
			b = append(b, p.Text)
		}
		return strings.Join(b, "\n")
	}
	return ""
}

// specPlan is the plan an ExitSpecMode call proposes for approval
// (input {plan}, a title first when given); its prompt is specOptions.
func specPlan(tool string, input json.RawMessage) (string, bool) {
	if tool != "ExitSpecMode" {
		return "", false
	}
	var in struct {
		Plan  string `json:"plan"`
		Title string `json:"title"`
	}
	if json.Unmarshal(input, &in) != nil || strings.TrimSpace(in.Plan) == "" {
		return "", false
	}
	if in.Title != "" {
		return "# " + strings.TrimSpace(in.Title) + "\n\n" + strings.TrimSpace(in.Plan), true
	}
	return strings.TrimSpace(in.Plan), true
}

// specOptions are the rows of the prompt ExitSpecMode raises, seen live
// (1.5.0, 2026-11-10): a box under "Propose Specification "<title>"" with
// keys 1-5 or arrows and Enter. The third row ends with a context saving
// that changes per session ("(1% context savings)"), left out here.
// Proceeding opens a second picker for the autonomy to run it with
// (Manual Approvals / Low / Medium / High), which the screen net shows.
var specOptions = []string{
	"Proceed with the proposal",
	"Proceed with comment",
	"Proceed and handoff to a new session",
	"Manually edit spec (open via system default)",
	"No and explain why",
}

// droidAsk is the question an AskUser or ExitSpecMode call raises.
func droidAsk(tool string, input json.RawMessage) (*harness.QuestionBody, bool) {
	if q, ok := askQuestion(tool, input); ok {
		return q, true
	}
	plan, ok := specPlan(tool, input)
	if !ok {
		return nil, false
	}
	title := strings.TrimPrefix(strings.SplitN(plan, "\n", 2)[0], "# ")
	q := harness.Question{ID: "0", Prompt: `Propose Specification "` + strings.TrimSpace(title) + `"`}
	for _, l := range specOptions {
		q.Options = append(q.Options, harness.Choice{ID: l, Label: l})
	}
	return &harness.QuestionBody{Questions: []harness.Question{q},
		Approve: specOptions[0], Deny: specOptions[len(specOptions)-1]}, true
}

// droidAnswered reads an AskUser result, or ExitSpecMode's
// {"approved": bool, ...}: approved is the proposal taken, else the spec
// was turned down.
func droidAnswered(tool string, input, response json.RawMessage) map[string][]string {
	if tool != "ExitSpecMode" {
		return askAnswered(tool, input, response)
	}
	var r struct {
		Approved *bool `json:"approved"`
	}
	if json.Unmarshal([]byte(responseText(response)), &r) != nil || r.Approved == nil {
		return nil
	}
	if *r.Approved {
		return map[string][]string{"0": {specOptions[0]}}
	}
	return map[string][]string{"0": {specOptions[len(specOptions)-1]}}
}
