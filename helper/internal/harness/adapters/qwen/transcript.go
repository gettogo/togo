package qwen

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// transcript maps Qwen Code's chat log. Records:
//
//	user        provenance real_user: a prompt (others are system text)
//	assistant   message.parts: text, thought text, functionCall {id, name, args}
//	tool_result message.parts functionResponse {id, name, response}, toolCallResult.status
//	system      subtype ui_telemetry (api errors), slash_command, snapshots
//
// A turn starts at a prompt and ends at an assistant record with no
// function call, or at the next prompt.
type transcript struct {
	env harness.Env

	mu      sync.Mutex
	state   map[string]string
	turn    string
	started time.Time
	steps   int
	tools   map[string]harness.Item
	n       int
}

func newTranscript(env harness.Env) *transcript {
	return &transcript{env: env, state: map[string]string{}, tools: map[string]harness.Item{}}
}

func (t *transcript) Name() string { return MapperTranscript }

func (t *transcript) State() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]string, len(t.state))
	for k, v := range t.state {
		out[k] = v
	}
	return out
}

type part struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought"`
	Call    *struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
	Response *struct {
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Response json.RawMessage `json:"response"`
	} `json:"functionResponse"`
}

type record struct {
	UUID       string `json:"uuid"`
	SessionID  string `json:"sessionId"`
	Timestamp  string `json:"timestamp"`
	Type       string `json:"type"`
	Provenance string `json:"provenance"`
	Subtype    string `json:"subtype"`
	Model      string `json:"model"`
	Message    struct {
		Parts []part `json:"parts"`
	} `json:"message"`
	Result *struct {
		Status string `json:"status"`
	} `json:"toolCallResult"`
	System struct {
		UIEvent map[string]any `json:"uiEvent"`
	} `json:"systemPayload"`
}

func (t *transcript) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var r record
	if json.Unmarshal(rec.Data, &r) != nil || r.UUID == "" {
		return nil, nil
	}
	hookstyle.MarkLive(t.env.SessionID)
	at := stamp(r.Timestamp, rec.At)
	t.mu.Lock()
	defer t.mu.Unlock()
	if r.SessionID != "" {
		t.state["harness_session"] = r.SessionID
	}
	switch r.Type {
	case "user":
		if r.Provenance != "real_user" {
			return nil, nil
		}
		return t.prompt(r, at), nil
	case "assistant":
		if r.Model != "" {
			t.state["model"] = r.Model
		}
		return t.assistant(r, at)
	case "tool_result":
		return t.result(r, at), nil
	case "system":
		if ev := r.System.UIEvent; r.Subtype == "ui_telemetry" && ev["event.name"] == "qwen-code.api_error" {
			msg, _ := ev["error_message"].(string)
			return []harness.Item{{ID: r.UUID, TurnID: t.turn, Kind: harness.KindNotice, At: at, Notice: &harness.NoticeBody{
				Level: "error", Code: harness.NoticeError, Text: hookstyle.Truncate(firstOf(msg, "The model request failed"), harness.MaxTitle)}}}, nil
		}
	}
	return nil, nil
}

func (t *transcript) prompt(r record, at time.Time) []harness.Item {
	var items []harness.Item
	if t.turn != "" {
		items = append(items, t.close(at, harness.StateOK)...)
	}
	t.turn, t.started, t.steps = r.UUID+".turn", at, 0
	t.state["working"], t.state["idle"] = "true", "false"
	var text []string
	for _, p := range r.Message.Parts {
		if p.Text != "" && !p.Thought {
			text = append(text, p.Text)
		}
	}
	items = append(items, harness.Item{ID: r.UUID, TurnID: t.turn, Kind: harness.KindUser, At: at,
		User: &harness.UserBody{Text: hookstyle.Clip(strings.Join(text, "\n")), Source: "unknown"}})
	return append(items, t.turnItem(harness.StateRunning, nil))
}

func (t *transcript) assistant(r record, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	var thought, text []string
	calls := false
	for _, p := range r.Message.Parts {
		switch {
		case p.Call != nil:
			calls = true
			x, op := t.call(p, at)
			items = append(items, x)
			ops = append(ops, op...)
		case p.Thought && p.Text != "":
			thought = append(thought, p.Text)
		case p.Text != "":
			text = append(text, p.Text)
		}
	}
	var head []harness.Item
	if len(thought) > 0 {
		head = append(head, harness.Item{ID: r.UUID + ":thought", TurnID: t.turn, Kind: harness.KindThinking, At: at,
			Thinking: &harness.TextBody{Text: hookstyle.Clip(strings.Join(thought, "\n"))}})
	}
	if len(text) > 0 {
		head = append(head, harness.Item{ID: r.UUID + ":text", TurnID: t.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: hookstyle.Clip(strings.Join(text, "\n\n"))}})
	}
	items = append(head, items...)
	if !calls && len(text) > 0 && t.turn != "" {
		items = append(items, t.close(at, harness.StateOK)...)
	} else if t.turn != "" {
		items = append(items, t.turnItem(harness.StateRunning, nil))
	}
	return items, ops
}

// call starts a tool item; todo_write also replaces the plan.
func (t *transcript) call(p part, at time.Time) (harness.Item, []harness.Op) {
	c := p.Call
	id := c.ID
	if id == "" {
		t.n++
		id = fmt.Sprintf("%s:call-%d", t.turn, t.n)
	}
	title, cat, subj := hookstyle.ToolTitle(c.Name, c.Args)
	var args struct {
		Description string `json:"description"`
		Todos       []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	_ = json.Unmarshal(c.Args, &args)
	if c.Name == "agent" {
		cat = harness.ToolAgent
		title = firstOf(args.Description, title)
	}
	t.steps++
	x := harness.Item{ID: id, TurnID: t.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: id, Name: c.Name, Title: hookstyle.Truncate(title, harness.MaxTitle), Category: cat, Subject: subj,
		State: harness.StateRunning, Started: at}}
	t.tools[id] = x
	if c.Name != "todo_write" {
		return x, nil
	}
	rows := make([]harness.Row, 0, len(args.Todos))
	for _, td := range args.Todos {
		state := harness.RowPending
		switch td.Status {
		case "in_progress":
			state = harness.RowActive
		case "completed":
			state = harness.RowDone
		case "cancelled":
			state = harness.RowSkipped
		}
		rows = append(rows, harness.Row{ID: td.ID, Title: hookstyle.Truncate(td.Content, harness.MaxTitle), State: state})
	}
	return x, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}}
}

func (t *transcript) result(r record, at time.Time) []harness.Item {
	var items []harness.Item
	for _, p := range r.Message.Parts {
		if p.Response == nil {
			continue
		}
		x, ok := t.tools[p.Response.ID]
		if !ok {
			continue
		}
		delete(t.tools, p.Response.ID)
		b := *x.Tool
		out, failed := hookstyle.ToolOutput(p.Response.Response)
		b.Output, b.Ended, b.State = out, &at, harness.StateOK
		if failed || (r.Result != nil && r.Result.Status != "" && r.Result.Status != "success") {
			b.State = harness.StateFailed
			if r.Result != nil && r.Result.Status == "cancelled" {
				b.State = harness.StateCancelled
			}
		}
		x.Tool = &b
		items = append(items, x)
	}
	return items
}

// close ends the turn; calls still open are interrupted.
func (t *transcript) close(at time.Time, state string) []harness.Item {
	var items []harness.Item
	for id, x := range t.tools {
		b := *x.Tool
		b.State, b.Ended = harness.StateInterrupted, &at
		x.Tool = &b
		items = append(items, x)
		delete(t.tools, id)
	}
	items = append(items, t.turnItem(state, &at))
	t.turn = ""
	t.state["working"], t.state["idle"] = "false", "true"
	return items
}

func (t *transcript) turnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: t.turn, TurnID: t.turn, Kind: harness.KindTurn, At: t.started,
		Turn: &harness.TurnBody{State: state, Started: t.started, Ended: ended, Steps: t.steps}}
}

func stamp(s string, fallback time.Time) time.Time {
	if v, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return v.UTC()
	}
	if !fallback.IsZero() {
		return fallback.UTC()
	}
	return time.Now().UTC()
}

func firstOf(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
