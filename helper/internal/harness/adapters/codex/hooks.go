package codex

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Hooks maps Codex hook payloads (SessionStart, UserPromptSubmit,
// PreToolUse, PostToolUse, PermissionRequest, Stop). It always keeps the
// session state; it draws items only while the session's app-server feed
// is not connected, so a Codex started without the app-server still gets a
// chat (prompts, tools, final replies) and a connected one is not drawn
// twice.
type Hooks struct {
	env harness.Env

	mu      sync.Mutex
	state   map[string]string
	turn    string
	started time.Time
	steps   map[string]bool
	running map[string][]string // tool name -> running call ids, oldest first
	tools   map[string]harness.Item
	n       int
}

// NewHooks returns a hooks mapper for one session.
func NewHooks(env harness.Env) *Hooks {
	return &Hooks{env: env, state: map[string]string{"working": "false", "idle": "true"},
		steps: map[string]bool{}, running: map[string][]string{}, tools: map[string]harness.Item{}}
}

func (h *Hooks) Name() string { return MapperHooks }

func (h *Hooks) State() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.state))
	for k, v := range h.state {
		out[k] = v
	}
	return out
}

type hookPayload struct {
	Event        string          `json:"hook_event_name"`
	SessionID    string          `json:"session_id"`
	TurnID       string          `json:"turn_id"`
	Model        string          `json:"model"`
	Mode         string          `json:"permission_mode"`
	Prompt       string          `json:"prompt"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolUseID    string          `json:"tool_use_id"`
	CallID       string          `json:"call_id"`
	ToolResponse json.RawMessage `json:"tool_response"`
	LastMessage  *string         `json:"last_assistant_message"`
}

func (h *Hooks) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var p hookPayload
	if json.Unmarshal(rec.Data, &p) != nil {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.SessionID != "" {
		h.state["harness_session"] = p.SessionID
	}
	if p.Model != "" {
		h.state["model"] = p.Model
	}
	draw := !isLive(h.env.SessionID)
	switch p.Event {
	case "UserPromptSubmit":
		h.begin(p.TurnID, at)
		if !draw {
			return nil, nil
		}
		u := harness.Item{ID: h.turn + ":user", TurnID: h.turn, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: clip(p.Prompt), Source: "unknown"}}
		return []harness.Item{u, h.turnItem(harness.StateRunning, nil)}, nil
	case "PreToolUse":
		if h.turn == "" {
			h.begin(p.TurnID, at)
		}
		x := h.toolStart(p, at)
		if !draw {
			return nil, nil
		}
		return []harness.Item{x, h.turnItem(harness.StateRunning, nil)}, nil
	case "PostToolUse":
		x, ok := h.toolEnd(p, at)
		if !ok || !draw {
			return nil, nil
		}
		return []harness.Item{x}, nil
	case "PermissionRequest":
		id := h.callID(p, false)
		x, ok := h.tools[id]
		if !ok || !draw {
			return nil, nil
		}
		b := *x.Tool
		b.State = harness.StateWaiting
		x.Tool = &b
		h.tools[id] = x
		return []harness.Item{x}, nil
	case "Stop":
		h.state["working"], h.state["idle"] = "false", "true"
		if !draw || h.turn == "" {
			return nil, nil
		}
		items := []harness.Item{h.turnItem(harness.StateOK, ptr(at))}
		if p.LastMessage != nil && *p.LastMessage != "" {
			items = append([]harness.Item{{ID: h.turn + ":reply", TurnID: h.turn, Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: clip(*p.LastMessage)}}}, items...)
		}
		return items, nil
	}
	return nil, nil
}

func (h *Hooks) begin(turn string, at time.Time) {
	if turn == "" {
		h.n++
		turn = "turn-" + strconv.Itoa(h.n)
	}
	if turn != h.turn {
		h.turn, h.started = turn, at
		h.steps = map[string]bool{}
		h.running = map[string][]string{}
		h.tools = map[string]harness.Item{}
	}
	h.state["turn"] = turn
	h.state["working"], h.state["idle"] = "true", "false"
}

func (h *Hooks) turnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: h.turn, TurnID: h.turn, Kind: harness.KindTurn, At: h.started,
		Turn: &harness.TurnBody{State: state, Started: h.started, Ended: ended, Steps: len(h.steps)}}
}

// callID finds the call a payload is about: its own id, else the oldest
// running call of the same tool (start: a fresh id).
func (h *Hooks) callID(p hookPayload, start bool) string {
	if p.ToolUseID != "" {
		return p.ToolUseID
	}
	if p.CallID != "" {
		return p.CallID
	}
	if start {
		h.n++
		return h.turn + ":tool-" + strconv.Itoa(h.n)
	}
	if ids := h.running[p.ToolName]; len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (h *Hooks) toolStart(p hookPayload, at time.Time) harness.Item {
	id := h.callID(p, true)
	h.running[p.ToolName] = append(h.running[p.ToolName], id)
	h.steps[id] = true
	title, cat, subj := hookTool(p.ToolName, p.ToolInput)
	x := harness.Item{ID: id, TurnID: h.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: id, Name: p.ToolName, Title: truncate(title, harness.MaxTitle), Category: cat, Subject: subj,
		State: harness.StateRunning, Started: at}}
	h.tools[id] = x
	return x
}

func (h *Hooks) toolEnd(p hookPayload, at time.Time) (harness.Item, bool) {
	id := h.callID(p, false)
	x, ok := h.tools[id]
	if !ok {
		return harness.Item{}, false
	}
	ids := h.running[p.ToolName]
	for i, v := range ids {
		if v == id {
			h.running[p.ToolName] = append(ids[:i:i], ids[i+1:]...)
			break
		}
	}
	delete(h.tools, id)
	b := *x.Tool
	b.State = harness.StateOK
	b.Ended = ptr(at)
	var s string
	if json.Unmarshal(p.ToolResponse, &s) == nil {
		b.Output = tail(s, harness.MaxToolOutput)
	} else if len(p.ToolResponse) > 0 && string(p.ToolResponse) != "null" {
		b.Output = tail(string(p.ToolResponse), harness.MaxToolOutput)
	}
	x.Tool = &b
	return x, true
}

// hookTool names a hook's tool call.
func hookTool(name string, input json.RawMessage) (string, string, *wire.Subject) {
	var in struct {
		Command  json.RawMessage `json:"command"`
		FilePath string          `json:"file_path"`
		Path     string          `json:"path"`
	}
	json.Unmarshal(input, &in)
	var cmd string
	if json.Unmarshal(in.Command, &cmd) != nil {
		var argv []string
		if json.Unmarshal(in.Command, &argv) == nil {
			cmd = strings.Join(argv, " ")
		}
	}
	switch {
	case cmd != "":
		cmd = innerCommand(cmd)
		return cmd, harness.ToolShell, &wire.Subject{Kind: wire.SubjectCommand, Text: truncate(cmd, wire.MaxSubjectText)}
	case name == "apply_patch" || name == "Edit" || name == "Write":
		return "Edit files", harness.ToolEdit, nil
	case in.FilePath != "" || in.Path != "":
		p := in.FilePath
		if p == "" {
			p = in.Path
		}
		return name + " " + p, harness.ToolOther, &wire.Subject{Kind: wire.SubjectFile, Text: truncate(p, wire.MaxSubjectText)}
	}
	if strings.HasPrefix(name, "mcp__") {
		return strings.TrimPrefix(name, "mcp__"), harness.ToolMCP, nil
	}
	return name, harness.ToolOther, nil
}
