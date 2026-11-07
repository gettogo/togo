package auggie

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// Hooks maps Auggie hook payloads. Tool hooks carry no call id, so a
// PostToolUse closes the oldest running call of the same tool.
type Hooks struct {
	env harness.Env

	mu      sync.Mutex
	state   map[string]string
	seq     int
	turn    *turn
	running []harness.Item
}

type turn struct {
	id      string
	started time.Time
	steps   int
	files   map[string]bool
}

// NewHooks returns a mapper for one session.
func NewHooks(env harness.Env) *Hooks {
	return &Hooks{env: env, state: map[string]string{"working": "false"}}
}

func (h *Hooks) Name() string { return MapperHooks }

// State reports harness_session (the conversation id), turn and working.
func (h *Hooks) State() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.state))
	for k, v := range h.state {
		out[k] = v
	}
	return out
}

type payload struct {
	Event        string          `json:"hook_event_name"`
	Conversation string          `json:"conversation_id"`
	Prompt       string          `json:"user_prompt"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolOutput   json.RawMessage `json:"tool_output"`
	ToolError    json.RawMessage `json:"tool_error"`
	FileChanges  []struct {
		Path       string `json:"path"`
		ChangeType string `json:"changeType"`
	} `json:"file_changes"`
	NoticeType string `json:"notification_type"`
	NoticeText string `json:"notification_message"`
	StopCause  string `json:"agent_stop_cause"`
	Exchange   *struct {
		Text string `json:"agentTextResponse"`
	} `json:"_exchange"`
	ConvData *struct {
		Text string `json:"agentTextResponse"`
	} `json:"conversation"`
}

func (h *Hooks) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var p payload
	if json.Unmarshal(rec.Data, &p) != nil || p.Event == "" {
		return nil, nil
	}
	at := rec.At.UTC()
	if rec.At.IsZero() {
		at = time.Now().UTC()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	if p.Conversation != "" {
		h.state["harness_session"] = p.Conversation
	}
	switch p.Event {
	case "PromptSubmit":
		items := h.startTurn(at)
		if p.Prompt != "" {
			items = append(items, harness.Item{ID: "u-" + h.turn.id, TurnID: h.turn.id, Kind: harness.KindUser, At: at,
				User: &harness.UserBody{Text: x.Clip(p.Prompt), Source: "unknown"}})
		}
		return items, nil
	case "PreToolUse":
		return h.toolStart(p, at), nil
	case "PostToolUse":
		return h.toolDone(p, at), nil
	case "Notification":
		if p.NoticeText == "" {
			return nil, nil
		}
		return []harness.Item{{ID: "n-" + strconv.Itoa(h.seq), TurnID: h.turnID(), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: p.NoticeType, Text: x.Title(p.NoticeText)}}}, nil
	case "Stop":
		var items []harness.Item
		text := ""
		if p.Exchange != nil {
			text = p.Exchange.Text
		} else if p.ConvData != nil {
			text = p.ConvData.Text
		}
		if text != "" {
			items = append(items, harness.Item{ID: "a-" + strconv.Itoa(h.seq), TurnID: h.turnID(), Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: x.Clip(text)}})
		}
		state := harness.StateOK
		if p.StopCause != "" && p.StopCause != "end_turn" && p.StopCause != "completed" {
			state = harness.StateInterrupted
		}
		return append(items, h.endTurn(state, at)...), nil
	case "SessionEnd":
		return h.endTurn(harness.StateInterrupted, at), nil
	}
	return nil, nil
}

func (h *Hooks) turnID() string {
	if h.turn == nil {
		return ""
	}
	return h.turn.id
}

func (h *Hooks) turnItem(state string, ended *time.Time) harness.Item {
	t := h.turn
	return harness.Item{ID: t.id, TurnID: t.id, Kind: harness.KindTurn, At: t.started,
		Turn: &harness.TurnBody{State: state, Started: t.started, Ended: ended, Steps: t.steps, FilesChanged: len(t.files)}}
}

func (h *Hooks) startTurn(at time.Time) []harness.Item {
	items := h.endTurn(harness.StateInterrupted, at)
	h.turn = &turn{id: "turn-" + strconv.FormatInt(at.UnixMilli(), 36) + "-" + strconv.Itoa(h.seq), started: at, files: map[string]bool{}}
	h.state["turn"], h.state["working"] = h.turn.id, "true"
	return append(items, h.turnItem(harness.StateRunning, nil))
}

func (h *Hooks) endTurn(state string, at time.Time) []harness.Item {
	h.state["working"] = "false"
	var items []harness.Item
	for _, t := range h.running {
		tb := *t.Tool
		tb.State, tb.Ended = harness.StateInterrupted, x.Ptr(at)
		t.Tool = &tb
		items = append(items, t)
	}
	h.running = nil
	if h.turn != nil {
		items = append(items, h.turnItem(state, x.Ptr(at)))
		h.turn = nil
	}
	return items
}

func (h *Hooks) toolStart(p payload, at time.Time) []harness.Item {
	var items []harness.Item
	if h.turn == nil {
		items = h.startTurn(at)
	}
	h.turn.steps++
	var in struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	json.Unmarshal(p.ToolInput, &in)
	title := p.ToolName
	if v := firstOf(in.Command, x.Rel(h.env.Cwd, in.Path)); v != "" {
		title += " " + v
	}
	cat := x.Category(p.ToolName)
	id := "tool-" + strconv.Itoa(h.seq)
	it := harness.Item{ID: id, TurnID: h.turn.id, Kind: harness.KindTool, At: at,
		Tool: &harness.ToolBody{CallID: id, Name: p.ToolName, Title: x.Title(title), Category: cat, State: harness.StateRunning, Started: at}}
	h.running = append(h.running, it)
	return append(items, it)
}

func (h *Hooks) toolDone(p payload, at time.Time) []harness.Item {
	i := -1
	for j, t := range h.running {
		if t.Tool.Name == p.ToolName {
			i = j
			break
		}
	}
	if i < 0 {
		return nil
	}
	it := h.running[i]
	h.running = append(h.running[:i], h.running[i+1:]...)
	tb := *it.Tool
	tb.State, tb.Ended = harness.StateOK, x.Ptr(at)
	if s := rawText(p.ToolError); s != "" {
		tb.State, tb.Output = harness.StateFailed, x.Tail(s, harness.MaxToolOutput)
	} else {
		tb.Output = x.Tail(rawText(p.ToolOutput), harness.MaxToolOutput)
	}
	for _, f := range p.FileChanges {
		tb.Files = append(tb.Files, harness.FileChange{Path: x.Rel(h.env.Cwd, f.Path)})
		if h.turn != nil {
			h.turn.files[f.Path] = true
		}
	}
	it.Tool = &tb
	return []harness.Item{it}
}

// rawText reads a value that is a string, null or any other JSON.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
