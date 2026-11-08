// Package turns builds chat items from a harness's stored messages: a
// prompt starts a turn, tool calls run inside it until their results come,
// and a final reply ends it. Goose and Crush share it; both keep whole
// messages in sqlite rather than a stream of events.
package turns

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// Builder keeps the open turn and its running tools. It is not safe for
// concurrent use; the mapper that owns it locks.
type Builder struct {
	turn    string
	started time.Time
	steps   int
	tools   map[string]harness.Item
	// Working is "true" while a turn runs, "false" after one ended, ""
	// before any; WorkingAt stamps the last change.
	Working   string
	WorkingAt string
}

// New returns an empty builder.
func New() *Builder { return &Builder{tools: map[string]harness.Item{}} }

// Reset forgets the open turn (another session's history follows).
func (b *Builder) Reset() {
	b.turn, b.steps, b.Working, b.WorkingAt = "", 0, "", ""
	b.tools = map[string]harness.Item{}
}

// Open reports whether a turn runs.
func (b *Builder) Open() bool { return b.turn != "" }

// User starts a turn with the prompt, closing any open one.
func (b *Builder) User(id, text string, at time.Time) []harness.Item {
	var items []harness.Item
	if b.turn != "" {
		items = b.Close(harness.StateOK, at)
	}
	b.turn, b.started, b.steps = id+".turn", at, 0
	b.working("true", at)
	items = append(items, harness.Item{ID: id, TurnID: b.turn, Kind: harness.KindUser, At: at,
		User: &harness.UserBody{Text: hookstyle.Clip(text), Source: "unknown"}})
	return append(items, b.TurnItem(harness.StateRunning, nil))
}

// Thinking is a reasoning block.
func (b *Builder) Thinking(id, text string, at time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: b.turn, Kind: harness.KindThinking, At: at,
		Thinking: &harness.TextBody{Text: hookstyle.Clip(text)}}
}

// Assistant is reply text.
func (b *Builder) Assistant(id, text string, at time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: b.turn, Kind: harness.KindAssistant, At: at,
		Assistant: &harness.TextBody{Text: hookstyle.Clip(text)}}
}

// Notice is an info line in the turn.
func (b *Builder) Notice(id, code, text string, at time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: b.turn, Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: code, Text: text}}
}

// ToolStart is a call that runs until ToolEnd. A call seen again (a
// message rewritten in place while its arguments stream) keeps its start
// and takes the newer title.
func (b *Builder) ToolStart(callID, name string, args json.RawMessage, at time.Time) harness.Item {
	if !json.Valid(args) {
		args = nil
	}
	title, cat, subj := hookstyle.ToolTitle(ShortName(name), args)
	if x, ok := b.tools[callID]; ok {
		body := *x.Tool
		body.Title, body.Category, body.Subject = hookstyle.Truncate(title, harness.MaxTitle), cat, subj
		x.Tool = &body
		b.tools[callID] = x
		return x
	}
	x := harness.Item{ID: callID, TurnID: b.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: callID, Name: name, Title: hookstyle.Truncate(title, harness.MaxTitle), Category: cat,
		Subject: subj, State: harness.StateRunning, Started: at}}
	b.tools[callID] = x
	b.steps++
	return x
}

// ToolEnd finishes a running call; ok is false for an unknown one.
func (b *Builder) ToolEnd(callID, output, state string, at time.Time) (harness.Item, bool) {
	x, ok := b.tools[callID]
	if !ok {
		return harness.Item{}, false
	}
	delete(b.tools, callID)
	body := *x.Tool
	body.State, body.Ended, body.Output = state, &at, hookstyle.Tail(output, harness.MaxToolOutput)
	x.Tool = &body
	return x, true
}

// Close ends the open turn; calls still running are interrupted.
func (b *Builder) Close(state string, at time.Time) []harness.Item {
	if b.turn == "" {
		return nil
	}
	var items []harness.Item
	for id, x := range b.tools {
		body := *x.Tool
		body.State, body.Ended = harness.StateInterrupted, &at
		x.Tool = &body
		items = append(items, x)
		delete(b.tools, id)
	}
	items = append(items, b.TurnItem(state, &at))
	b.turn = ""
	b.working("false", at)
	return items
}

// TurnItem is the open turn's header.
func (b *Builder) TurnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: b.turn, TurnID: b.turn, Kind: harness.KindTurn, At: b.started,
		Turn: &harness.TurnBody{State: state, Started: b.started, Ended: ended, Steps: b.steps}}
}

// TurnID is the open turn's id, or "".
func (b *Builder) TurnID() string { return b.turn }

func (b *Builder) working(v string, at time.Time) {
	b.Working, b.WorkingAt = v, harness.StampWorking(at)
}

// State copies the working flags into a mapper's state.
func (b *Builder) State(into map[string]string) {
	if b.Working == "" {
		return
	}
	into["working"] = b.Working
	into["idle"] = map[string]string{"true": "false", "false": "true"}[b.Working]
	into[harness.StateWorkingAt] = b.WorkingAt
}

// ShortName drops an extension prefix ("developer__shell" -> "shell").
func ShortName(name string) string {
	if i := strings.LastIndex(name, "__"); i >= 0 && i+2 < len(name) {
		return name[i+2:]
	}
	return name
}
