package opencode

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode 2.x asks through forms (the question tool's questions, an MCP
// server's input request) and permission requests. Both become question
// items with a needs row. The item id is "<request id>~<session id>", so
// the answer action can name the session the request belongs to (a child
// session's request is answered on that session).
//
// A form owned by "global" has no session: OpenCode asks an MCP server's
// input request that way. It belongs to this chat when it was asked in the
// folder the followed conversation works in, and its reply must name that
// folder (the answer action reads it from the "location" state).
//
// A permission has one question "0" with the options once, always and
// reject. A form's question ids are its field keys, marked with the type
// the reply needs: "key[]" a list, "key!" a boolean, "key#" a number, a
// bare key a string. The answer action's formAnswer builds the reply from
// them.

type form2 struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Title     string         `json:"title"`
	Metadata  map[string]any `json:"metadata"`
	Fields    []field2       `json:"fields"`
}

type field2 struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Hidden      bool   `json:"hidden"`
	Custom      *bool  `json:"custom"`
	URL         string `json:"url"`
	Options     []struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

func needID(req, sess string) string { return req + "~" + sess }

// replyKey is a field name the answer action's form filter accepts (the
// engine's formKey).
var replyKey = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// globalOwner is the session id of a form no session owns (seen on 2.2.5
// and in OpenCode's MCP client, GLOBAL_ELICITATION_SESSION_ID).
const globalOwner = "global"

// stateLocation is the state key naming the folder the followed
// conversation works in, spelled as the server spells it.
const stateLocation = "location"

// located is a conversation and the folder its events name.
type located struct{ sess, dir string }

// locate runs after every event. It learns the followed conversation's
// folder from that conversation's own events, and forgets it as soon as
// the chat follows another conversation: the folder of the one left behind
// must not decide which global forms are shown.
func (e *Events2) locate(sess string, loc *location2) {
	if sess != "" && sess == e.main && loc != nil && loc.Directory != "" {
		e.loc = located{sess, loc.Directory}
	}
	if e.loc.sess != e.main {
		e.loc = located{}
	}
	if e.loc.dir == "" {
		delete(e.state, stateLocation)
		return
	}
	e.state[stateLocation] = e.loc.dir
}

// here reports whether an event happened in this chat's folder: the
// followed conversation's, or the session's own before any event named
// one.
func (e *Events2) here(loc *location2) bool {
	if loc == nil || loc.Directory == "" {
		return false
	}
	if e.loc.sess == e.main && e.loc.dir != "" {
		return loc.Directory == e.loc.dir
	}
	return e.env.Cwd != "" && filepath.Clean(loc.Directory) == filepath.Clean(e.env.Cwd)
}

// asPermission marks a permission's question (the first item) as one a
// notification answers: Approve allows it once, Deny rejects it. "Always
// allow" stays in the app.
func asPermission(items []harness.Item) {
	if len(items) > 0 && items[0].Question != nil {
		items[0].Question.Approve, items[0].Question.Deny = "once", "reject"
	}
}

func (e *Events2) permission2(d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if d.ID == "" || !e.follows(d.SessionID) {
		return nil, nil
	}
	what := strings.ReplaceAll(d.Action, "_", " ")
	if what == "" {
		what = "this action"
	}
	prompt := "Allow " + what
	if len(d.Resources) > 0 {
		prompt += ": " + strings.Join(d.Resources, ", ")
	}
	q := harness.Question{ID: "0", Header: "Permission", Prompt: truncate(prompt, 1000), Options: []harness.Choice{
		{ID: "once", Label: "Allow once"},
		{ID: "always", Label: "Always allow"},
		{ID: "reject", Label: "Deny"},
	}}
	items, ops := e.need2(needID(d.ID, d.SessionID), d.SessionID, []harness.Question{q}, true, "Allow "+what, q.Prompt, at)
	asPermission(items)
	if d.Source != nil && d.Source.Type == "tool" {
		if x, ok := e.tools[d.Source.ID]; ok {
			b := *x.Tool
			b.State = harness.StateWaiting
			x.Tool = &b
			e.tools[d.Source.ID] = x
			items = append(items, x)
		}
	}
	return items, ops
}

func (e *Events2) form(d ev2, loc *location2, at time.Time) ([]harness.Item, []harness.Op) {
	f := d.Form
	if f == nil || f.ID == "" {
		return nil, nil
	}
	// The item keeps the form's own owner in its id, which the reply is
	// sent to; a global form is shown in the followed conversation's turn.
	in := f.SessionID
	if f.SessionID == globalOwner {
		if !e.here(loc) {
			return nil, nil
		}
		in = e.main
	} else if !e.follows(f.SessionID) {
		return nil, nil
	}
	question := f.Metadata["kind"] == "question"
	answerable := true
	var qs []harness.Question
	for i, fl := range f.Fields {
		if fl.Key == "" && question {
			// The helper strips every "key" field from what a harness
			// sends (it names provider keys elsewhere), so the field key
			// is gone; the question tool names its fields q0, q1... (seen
			// on 2.2.5).
			fl.Key = "q" + strconv.Itoa(i)
		}
		if fl.Hidden || len(qs) == MaxQuestions {
			continue
		}
		if !replyKey.MatchString(fl.Key) {
			// A reply cannot name this field, so it cannot be complete:
			// the form is shown whole and answered in the terminal.
			answerable = false
			fl.Key = "?" + strconv.Itoa(i)
		}
		q := harness.Question{ID: fl.Key, Header: fl.Title, Prompt: fl.Description}
		if q.Prompt == "" {
			q.Prompt, q.Header = fl.Title, ""
		}
		for _, o := range fl.Options {
			label := o.Label
			if label == "" {
				label = o.Value
			}
			q.Options = append(q.Options, harness.Choice{ID: o.Value, Label: label, Detail: o.Description})
		}
		switch fl.Type {
		case "multiselect":
			q.ID += "[]"
			q.Multi = true
			q.AllowText = fl.Custom != nil && *fl.Custom
		case "boolean":
			q.ID += "!"
			q.Options = []harness.Choice{{ID: "true", Label: "Yes"}, {ID: "false", Label: "No"}}
		case "number", "integer":
			q.ID += "#"
			q.AllowText = true
		case "external":
			q.Prompt = strings.TrimSpace(q.Prompt + " " + fl.URL)
		default:
			q.AllowText = len(q.Options) == 0 || fl.Custom == nil || *fl.Custom
		}
		qs = append(qs, q)
	}
	if len(qs) == 0 {
		return nil, nil
	}
	title := qs[0].Header
	if title == "" {
		title = qs[0].Prompt
	}
	if title == "" {
		title = f.Title
	}
	items, ops := e.need2(needID(f.ID, f.SessionID), in, qs, answerable, title, qs[0].Prompt, at)
	if tool, ok := f.Metadata["tool"].(map[string]any); ok {
		if id, _ := tool["id"].(string); id != "" {
			if x, ok := e.tools[id]; ok {
				b := *x.Tool
				b.State = harness.StateWaiting
				x.Tool = &b
				e.tools[id] = x
				items = append(items, x)
			}
		}
	}
	return items, ops
}

// need2 opens a question item and its needs row. One that cannot be
// answered from the phone opens the terminal instead of offering Answer.
func (e *Events2) need2(id, sess string, qs []harness.Question, answerable bool, title, subtitle string, at time.Time) ([]harness.Item, []harness.Op) {
	body := &harness.QuestionBody{Questions: qs}
	row := harness.Row{ID: id, Title: truncate(title, harness.MaxTitle), Subtitle: truncate(subtitle, harness.MaxTitle),
		State: harness.StateWaiting, Started: ptr(at)}
	if answerable {
		row.Actions = []harness.ActionRef{{ID: harness.ActionAnswer, Label: "Answer", Role: harness.RolePrimary}}
	} else {
		no := false
		body.Answerable = &no
		row.Open = &harness.Open{Kind: harness.OpenTerminal, Ref: e.env.SessionID}
	}
	x := harness.Item{ID: id, TurnID: e.turnFor(sess), Parent: e.parent(sess), Kind: harness.KindQuestion, At: at, Question: body}
	e.open[id] = x
	return []harness.Item{x}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionNeeds, Row: &row}}
}

func (e *Events2) permissionReplied(d ev2) ([]harness.Item, []harness.Op) {
	id := needID(d.RequestID, d.SessionID)
	x, ok := e.open[id]
	if !ok {
		return nil, nil
	}
	delete(e.open, id)
	x = closeQuestion(x, func(string) []string { return []string{d.Reply} })
	return []harness.Item{x}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: id}}
}

func (e *Events2) formDone(kind string, d ev2) ([]harness.Item, []harness.Op) {
	id := needID(d.ID, d.SessionID)
	x, ok := e.open[id]
	if !ok {
		return nil, nil
	}
	delete(e.open, id)
	var pick func(string) []string
	if kind == "form.replied" {
		pick = func(qid string) []string { return answerValues(d.Answer[fieldKey(qid)]) }
	}
	x = closeQuestion(x, pick)
	return []harness.Item{x}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: id}}
}

// closeQuestion marks a question item answered; pick gives each question's
// answer, nil leaves them all empty.
func closeQuestion(x harness.Item, pick func(qid string) []string) harness.Item {
	body := *x.Question
	body.Answered = map[string][]string{}
	for _, q := range body.Questions {
		v := []string{}
		if pick != nil {
			if got := pick(q.ID); got != nil {
				v = got
			}
		}
		body.Answered[q.ID] = v
	}
	x.Question = &body
	return x
}

// fieldKey strips the reply type mark from a question id.
func fieldKey(qid string) string {
	for _, suf := range []string{"[]", "!", "#"} {
		if k, ok := strings.CutSuffix(qid, suf); ok {
			return k
		}
	}
	return qid
}

func answerValues(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case string:
		return []string{x}
	}
	return []string{fmt.Sprint(v)}
}
