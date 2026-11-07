package kimi

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

type loopEvent struct {
	Type        string          `json:"type"`
	StepUUID    string          `json:"stepUuid"`
	Part        part            `json:"part"`
	ToolCallID  string          `json:"toolCallId"`
	Name        string          `json:"name"`
	Args        json.RawMessage `json:"args"`
	Description string          `json:"description"`
	Finish      string          `json:"finishReason"`
	Display     struct {
		Kind      string `json:"kind"`
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Command   string `json:"command"`
	} `json:"display"`
	Result struct {
		Output  string `json:"output"`
		IsError bool   `json:"isError"`
	} `json:"result"`
}

func (w *Wire) loopEvent(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var e loopEvent
	if json.Unmarshal(raw, &e) != nil {
		return nil, nil
	}
	switch e.Type {
	case "step.begin":
		if w.turn != nil {
			w.turn.steps++
		}
	case "content.part":
		return w.content(e, at), nil
	case "tool.call":
		return w.toolCall(e, at)
	case "tool.result":
		return w.toolResult(e, at)
	case "step.end":
		// Protocol 1.4 has no turn.ended: the last step ends the turn.
		switch e.Finish {
		case "end_turn":
			return w.endTurn(harness.StateOK, at)
		case "interrupted":
			return w.endTurn(harness.StateInterrupted, at)
		}
	}
	return nil, nil
}

func (w *Wire) turnID() string {
	if w.turn == nil {
		return ""
	}
	return w.turn.id
}

// content appends a text or thinking part to its step's item.
func (w *Wire) content(e loopEvent, at time.Time) []harness.Item {
	kind, text := harness.KindAssistant, e.Part.Text
	if e.Part.Type == "think" {
		kind, text = harness.KindThinking, e.Part.Think
	} else if e.Part.Type != "text" {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	key := e.StepUUID + ":" + kind
	it := w.texts[key]
	if it == nil {
		it = &harness.Item{ID: key, TurnID: w.turnID(), Kind: kind, At: at}
		w.texts[key] = it
	}
	body := it.Assistant
	if kind == harness.KindThinking {
		body = it.Thinking
	}
	if body == nil {
		body = &harness.TextBody{}
	} else {
		body.Text += "\n\n"
	}
	body.Text = x.Clip(body.Text + text)
	if kind == harness.KindThinking {
		it.Thinking = body
	} else {
		it.Assistant = body
	}
	cp := *it
	b := *body
	if kind == harness.KindThinking {
		cp.Thinking = &b
	} else {
		cp.Assistant = &b
	}
	return []harness.Item{cp}
}

func (w *Wire) toolCall(e loopEvent, at time.Time) ([]harness.Item, []harness.Op) {
	if e.ToolCallID == "" {
		return nil, nil
	}
	title := e.Description
	if title == "" {
		title = e.Name
	}
	it := harness.Item{ID: e.ToolCallID, TurnID: w.turnID(), Kind: harness.KindTool, At: at,
		Tool: &harness.ToolBody{CallID: e.ToolCallID, Name: e.Name, Title: x.Title(title), Category: x.Category(e.Name),
			State: harness.StateRunning, Started: at}}
	if p := e.Display.Path; p != "" && (e.Display.Operation == "write" || e.Display.Operation == "edit" || it.Tool.Category == harness.ToolEdit || it.Tool.Category == harness.ToolWrite) {
		it.Tool.Files = []harness.FileChange{{Path: x.Rel(w.env.Cwd, p)}}
		if w.turn != nil {
			w.turn.files[p] = true
		}
	}
	w.tools[e.ToolCallID] = it
	items := []harness.Item{it}
	var ops []harness.Op
	switch e.Name {
	case "TodoList":
		pi, pops := w.plan(e.Args, at)
		items, ops = append(items, pi...), append(ops, pops...)
	case "Agent":
		var a struct {
			Description string `json:"description"`
			Type        string `json:"subagent_type"`
		}
		json.Unmarshal(e.Args, &a)
		name := a.Description
		if name == "" {
			name = "Subagent"
		}
		ag := harness.Item{ID: "agent-" + e.ToolCallID, TurnID: w.turnID(), Kind: harness.KindAgent, At: at,
			Agent: &harness.AgentBody{AgentID: e.ToolCallID, Name: x.Title(name), Role: a.Type, State: harness.StateRunning, Started: at}}
		w.tasks[e.ToolCallID] = ag
		items = append(items, ag)
		ops = append(ops, taskRow(ag))
	}
	return items, ops
}

func (w *Wire) toolResult(e loopEvent, at time.Time) ([]harness.Item, []harness.Op) {
	it, ok := w.tools[e.ToolCallID]
	if !ok {
		return nil, nil
	}
	delete(w.tools, e.ToolCallID)
	tb := *it.Tool
	tb.State, tb.Ended, tb.Output = harness.StateOK, x.Ptr(at), x.Tail(e.Result.Output, harness.MaxToolOutput)
	if e.Result.IsError {
		tb.State = harness.StateFailed
	}
	it.Tool = &tb
	items := []harness.Item{it}
	var ops []harness.Op
	fields := resultFields(e.Result.Output)
	task, running := fields["task_id"], fields["status"] == "running"
	if ag, ok := w.tasks[e.ToolCallID]; ok {
		// An Agent call: in the background it finishes later, by task id.
		ab := *ag.Agent
		if running && task != "" {
			delete(w.tasks, e.ToolCallID)
			w.tasks[task] = ag
		} else {
			ab.State, ab.Ended = tb.State, x.Ptr(at)
			ab.Summary = x.Truncate(e.Result.Output, harness.MaxTitle)
			ag.Agent = &ab
			delete(w.tasks, e.ToolCallID)
		}
		items, ops = append(items, ag), append(ops, taskRow(ag))
	} else if running && task != "" {
		bg := harness.Item{ID: "bg-" + task, TurnID: w.turnID(), Kind: harness.KindBackground, At: at,
			Background: &harness.BackgroundBody{JobID: task, Kind: harness.BackgroundShell, Title: x.Title(firstNonEmpty(fields["description"], it.Tool.Title)),
				State: harness.StateRunning, Started: at}}
		w.tasks[task] = bg
		items, ops = append(items, bg), append(ops, taskRow(bg))
	}
	return items, ops
}

// resultFields reads the "key: value" lines Kimi prints for started tasks.
func resultFields(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if ok && k != "" && !strings.Contains(k, " ") {
			if _, seen := m[k]; !seen {
				m[k] = strings.TrimSpace(v)
			}
		}
	}
	return m
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
