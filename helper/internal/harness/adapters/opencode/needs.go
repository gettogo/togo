package opencode

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Questions (the question tool) and permission requests both become
// question items with a needs row, answered with the answer action. A
// question's ids are "0", "1"... in order and its option ids are the labels
// OpenCode wants back; a permission has one question "0" with the options
// once, always and reject.

// MaxQuestions is how many questions of one request the answer action can
// send back.
const MaxQuestions = 4

type toolRef struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

func (e *Events) question(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Questions []struct {
			Question string `json:"question"`
			Header   string `json:"header"`
			Multiple bool   `json:"multiple"`
			Custom   *bool  `json:"custom"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
		Tool *toolRef `json:"tool"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ID == "" || len(p.Questions) == 0 || !e.follows(p.SessionID) {
		return nil, nil
	}
	qs := make([]harness.Question, 0, len(p.Questions))
	for i, q := range p.Questions {
		if i == MaxQuestions {
			break
		}
		hq := harness.Question{ID: strconv.Itoa(i), Header: q.Header, Prompt: q.Question, Multi: q.Multiple,
			AllowText: q.Custom == nil || *q.Custom}
		for _, o := range q.Options {
			hq.Options = append(hq.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
		}
		qs = append(qs, hq)
	}
	title := qs[0].Header
	if title == "" {
		title = qs[0].Prompt
	}
	return e.need(p.ID, p.SessionID, p.Tool, qs, title, qs[0].Prompt, at)
}

func (e *Events) permission(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ID         string         `json:"id"`
		SessionID  string         `json:"sessionID"`
		Permission string         `json:"permission"`
		Patterns   []string       `json:"patterns"`
		Metadata   map[string]any `json:"metadata"`
		Tool       *toolRef       `json:"tool"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ID == "" || !e.follows(p.SessionID) {
		return nil, nil
	}
	what := strings.ReplaceAll(p.Permission, "_", " ")
	if what == "" {
		what = "this action"
	}
	prompt := "Allow " + what
	if len(p.Patterns) > 0 {
		prompt += ": " + strings.Join(p.Patterns, ", ")
	}
	q := harness.Question{ID: "0", Header: "Permission", Prompt: truncate(prompt, 1000), Options: []harness.Choice{
		{ID: "once", Label: "Allow once"},
		{ID: "always", Label: "Always allow"},
		{ID: "reject", Label: "Deny"},
	}}
	items, ops := e.need(p.ID, p.SessionID, p.Tool, []harness.Question{q}, "Allow "+what, q.Prompt, at)
	// The tool that asked waits for the answer.
	if p.Tool != nil {
		if x, ok := e.tools[p.Tool.CallID]; ok {
			b := *x.Tool
			b.State = harness.StateWaiting
			x.Tool = &b
			e.tools[p.Tool.CallID] = x
			items = append(items, x)
		}
	}
	return items, ops
}

func (e *Events) need(id, sess string, tool *toolRef, qs []harness.Question, title, subtitle string, at time.Time) ([]harness.Item, []harness.Op) {
	turnID := ""
	if tool != nil {
		turnID = e.messages[tool.MessageID].turn
	}
	if turnID == "" {
		turnID = e.state["turn"]
	}
	x := harness.Item{ID: id, TurnID: turnID, Parent: e.parentOf(sess), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
	e.open[id] = x
	row := harness.Row{ID: id, Title: truncate(title, harness.MaxTitle), Subtitle: truncate(subtitle, harness.MaxTitle),
		State: harness.StateWaiting, Started: ptr(at),
		Actions: []harness.ActionRef{{ID: harness.ActionAnswer, Label: "Answer", Role: harness.RolePrimary}}}
	return []harness.Item{x}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionNeeds, Row: &row}}
}

// answered closes a question or permission, answered here or at the desk.
func (e *Events) answered(kind string, raw json.RawMessage) ([]harness.Item, []harness.Op) {
	var p struct {
		RequestID string     `json:"requestID"`
		Answers   [][]string `json:"answers"`
		Reply     string     `json:"reply"`
	}
	if json.Unmarshal(raw, &p) != nil || p.RequestID == "" {
		return nil, nil
	}
	x, ok := e.open[p.RequestID]
	if !ok {
		return nil, nil
	}
	delete(e.open, p.RequestID)
	body := *x.Question
	body.Answered = map[string][]string{}
	for i, q := range body.Questions {
		switch {
		case kind == "permission.replied":
			body.Answered[q.ID] = []string{p.Reply}
		case i < len(p.Answers):
			body.Answered[q.ID] = p.Answers[i]
		default:
			body.Answered[q.ID] = []string{}
		}
	}
	x.Question = &body
	return []harness.Item{x}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: p.RequestID}}
}

// dropOpen closes every open question and permission after the event
// stream reconnected: replies made at the desk meanwhile were missed, and
// a stale row must not stay answerable. Ones still pending are answered
// in the terminal.
func (e *Events) dropOpen() ([]harness.Item, []harness.Op) {
	ids := make([]string, 0, len(e.open))
	for id := range e.open {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var items []harness.Item
	var ops []harness.Op
	for _, id := range ids {
		x := e.open[id]
		delete(e.open, id)
		body := *x.Question
		body.Answered = map[string][]string{}
		for _, q := range body.Questions {
			body.Answered[q.ID] = []string{}
		}
		x.Question = &body
		items = append(items, x)
		ops = append(ops, harness.Op{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: id})
	}
	return items, ops
}
