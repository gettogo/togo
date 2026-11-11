package goose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// Goose stores what it asks the user as actionRequired parts (1.57, read
// from a real sessions.db):
//
//   - toolConfirmation {id (the tool call id), toolName, arguments, prompt}
//     in an assistant message while the CLI's cliclack select waits (approve
//     and smart_approve modes); toolConfirmationResponse {id, permission:
//     allow_once | always_allow | deny_once | cancel} in a hidden user
//     message once answered. Cancel is recorded as deny_once (seen live).
//   - elicitation {id, message, requested_schema} from an MCP server, and
//     elicitationResponse {id, user_data, action: accept | decline |
//     cancel}. The CLI collects the fields as line prompts and stores the
//     request only after the response, so it arrives answered.
//
// Both are hidden from the chat in Goose's own UI (userVisible false on the
// responses), so they are read before the visibility filter.

// Goose's cliclack select (goose-cli session/mod.rs
// prompt_tool_confirmation) asks "Goose would like to call the above tool,
// do you allow?", or "Do you allow this tool call?" with a prompt (a
// security finding), which drops Always Allow.

var permissionLabel = map[string]string{
	"allow_once":   "Allow",
	"always_allow": "Always Allow",
	"deny_once":    "Deny",
	"cancel":       "Cancel",
}

type actionData struct {
	ActionType string          `json:"actionType"`
	ID         string          `json:"id"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
	Prompt     *string         `json:"prompt"`
	Permission string          `json:"permission"`
	Message    string          `json:"message"`
	Schema     json.RawMessage `json:"requested_schema"`
	UserData   json.RawMessage `json:"user_data"`
	Action     string          `json:"action"`
}

// actions maps the actionRequired parts of a message.
func (m *db) actions(parts []part, at time.Time) []harness.Item {
	var items []harness.Item
	for _, p := range parts {
		if p.Type != "actionRequired" || len(p.Data) == 0 {
			continue
		}
		var d actionData
		if json.Unmarshal(p.Data, &d) != nil || d.ID == "" {
			continue
		}
		switch d.ActionType {
		case "toolConfirmation":
			items = append(items, m.confirmation(d, at))
		case "toolConfirmationResponse":
			if it, ok := m.settle("confirm-"+d.ID, map[string][]string{"0": labelOf(d.Permission)}); ok {
				items = append(items, it)
			}
		case "elicitation":
			items = append(items, m.elicitation(d, at)...)
		case "elicitationResponse":
			id := "elicit-" + d.ID
			got := elicitAnswers(d)
			if it, ok := m.settle(id, got); ok {
				items = append(items, it)
			} else {
				m.early[id] = got
			}
		}
	}
	return items
}

func labelOf(permission string) []string {
	if l, ok := permissionLabel[permission]; ok {
		return []string{l}
	}
	return []string{}
}

// confirmation is the tool approval Goose waits on: one question in the
// CLI's wording, Allow / Always Allow / Deny / Cancel.
func (m *db) confirmation(d actionData, at time.Time) harness.Item {
	id := "confirm-" + d.ID
	if a, ok := m.asks[id]; ok {
		return a
	}
	// The prompt names the call ("Allow shell: make?"), so a notification
	// says what it allows; the dialog on screen is linked by its options.
	q := harness.Question{ID: "0", Prompt: "Allow " + toolSummary(d.ToolName, d.Arguments) + "?"}
	if d.Prompt != nil && *d.Prompt != "" {
		q.Header = x.Title(*d.Prompt) // the security finding behind the ask
		q.Options = []harness.Choice{{ID: "Allow", Label: "Allow", Detail: "Allow the tool call once"},
			{ID: "Deny", Label: "Deny", Detail: "Deny the tool call"},
			{ID: "Cancel", Label: "Cancel", Detail: "Cancel the AI response and tool call"}}
	} else {
		q.Options = []harness.Choice{{ID: "Allow", Label: "Allow", Detail: "Allow the tool call once"},
			{ID: "Always Allow", Label: "Always Allow", Detail: "Always allow the tool call"},
			{ID: "Deny", Label: "Deny", Detail: "Deny the tool call"},
			{ID: "Cancel", Label: "Cancel", Detail: "Cancel the AI response and tool call"}}
	}
	it := harness.Item{ID: id, TurnID: m.b.TurnID(), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: []harness.Question{q}, Approve: "Allow", Deny: "Deny"}}
	m.asks[id] = it
	return it
}

// toolSummary names the tool and, for a command, the command: "shell: make".
func toolSummary(name string, args json.RawMessage) string {
	var a map[string]any
	json.Unmarshal(args, &a)
	if c, ok := a["command"].(string); ok && c != "" {
		return x.Title(name + ": " + c)
	}
	return x.Title(name)
}

// elicitation is an MCP server's form: one question per field, in the
// schema's order; an enum offers its values, a boolean Yes / No, anything
// else takes text.
func (m *db) elicitation(d actionData, at time.Time) []harness.Item {
	id := "elicit-" + d.ID
	if _, ok := m.asks[id]; ok {
		return nil
	}
	qs := schemaQuestions(d.Schema)
	if len(qs) == 0 {
		qs = []harness.Question{{ID: "0", Prompt: d.Message, Options: []harness.Choice{}, AllowText: true}}
	} else if d.Message != "" {
		qs[0].Prompt = d.Message + "\n" + qs[0].Prompt
	}
	it := harness.Item{ID: id, TurnID: m.b.TurnID(), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
	m.asks[id] = it
	if got, ok := m.early[id]; ok {
		delete(m.early, id)
		it, _ = m.settle(id, got)
	}
	return []harness.Item{it}
}

type schemaProp struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Enum        []any  `json:"enum"`
}

// schemaQuestions reads requested_schema.properties in document order.
func schemaQuestions(raw json.RawMessage) []harness.Question {
	var s struct {
		Properties json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(raw, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(s.Properties))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var qs []harness.Question
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return qs
		}
		name, _ := t.(string)
		var p schemaProp
		if dec.Decode(&p) != nil || name == "" {
			return qs
		}
		prompt := firstNonEmpty(p.Title, name)
		if p.Description != "" {
			prompt += " (" + p.Description + ")"
		}
		q := harness.Question{ID: name, Prompt: prompt, Options: []harness.Choice{}}
		switch {
		case len(p.Enum) > 0:
			for _, e := range p.Enum {
				v := fmt.Sprint(e)
				q.Options = append(q.Options, harness.Choice{ID: v, Label: v})
			}
		case p.Type == "boolean":
			q.Options = []harness.Choice{{ID: "Yes", Label: "Yes"}, {ID: "No", Label: "No"}}
		default:
			q.AllowText = true
		}
		qs = append(qs, q)
	}
	return qs
}

// elicitAnswers reads an elicitation response: accepted values per field,
// none when declined or cancelled.
func elicitAnswers(d actionData) map[string][]string {
	got := map[string][]string{}
	if d.Action != "accept" {
		return got
	}
	var data map[string]any
	json.Unmarshal(d.UserData, &data)
	for k, v := range data {
		switch t := v.(type) {
		case bool:
			got[k] = []string{map[bool]string{true: "Yes", false: "No"}[t]}
		case string:
			if strings.TrimSpace(t) != "" {
				got[k] = []string{t}
			}
		case nil:
		default:
			got[k] = []string{fmt.Sprint(t)}
		}
	}
	return got
}

// settle answers an open question item; every question left out gets an
// empty answer, so the card closes.
func (m *db) settle(id string, got map[string][]string) (harness.Item, bool) {
	it, ok := m.asks[id]
	if !ok || it.Question.Answered != nil {
		return harness.Item{}, false
	}
	qb := *it.Question
	qb.Answered = map[string][]string{}
	for _, q := range qb.Questions {
		qb.Answered[q.ID] = []string{}
		if v := got[q.ID]; len(v) > 0 {
			qb.Answered[q.ID] = v
		}
	}
	it.Question = &qb
	m.asks[id] = it
	return it, true
}

// closeOpen closes whatever still waits, when the turn ends without an
// answer on record (a new prompt, a failed turn).
func (m *db) closeOpen() []harness.Item {
	var items []harness.Item
	for id, it := range m.asks {
		if it.Question.Answered == nil {
			c, _ := m.settle(id, nil)
			items = append(items, c)
		}
	}
	return items
}
