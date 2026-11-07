package vibe

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// session maps Vibe's message journal. A user message starts a turn, an
// assistant message without tool calls ends it, tool messages finish the
// call named by tool_call_id. The journal has no timestamps, so items take
// the time they were read.
type session struct {
	env harness.Env

	mu      sync.Mutex
	state   map[string]string
	turn    string
	started time.Time
	steps   int
	tools   map[string]harness.Item
	n       int
}

func newSession(env harness.Env) *session {
	return &session{env: env, state: map[string]string{}, tools: map[string]harness.Item{}}
}

func (s *session) Name() string { return MapperSession }

func (s *session) State() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out
}

type message struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Reasoning json.RawMessage `json:"reasoning_content"`
	Injected  bool            `json:"injected"`
	MessageID string          `json:"message_id"`
	Boundary  string          `json:"context_boundary"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
	ToolResult *struct {
		Output    json.RawMessage `json:"output"`
		Cancelled bool            `json:"cancelled"`
	} `json:"tool_result"`
}

func (s *session) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var m message
	if json.Unmarshal(rec.Data, &m) != nil || m.Role == "" {
		return nil, nil
	}
	hookstyle.MarkLive(s.env.SessionID)
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	id := m.MessageID
	if id == "" {
		id = "vibe-" + strconv.Itoa(s.n)
	}
	switch m.Role {
	case "user":
		if m.Injected {
			return nil, nil
		}
		if m.Boundary == "compaction" {
			return []harness.Item{{ID: id, Kind: harness.KindNotice, At: at, Notice: &harness.NoticeBody{
				Level: "info", Code: harness.NoticeCompacted, Text: "Conversation compacted"}}}, nil
		}
		var items []harness.Item
		if s.turn != "" {
			items = s.close(at)
		}
		s.turn, s.started, s.steps = id+".turn", at, 0
		s.state["working"], s.state["idle"] = "true", "false"
		s.state[harness.StateWorkingAt] = harness.StampWorking(at)
		items = append(items, harness.Item{ID: id, TurnID: s.turn, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: hookstyle.Clip(text(m.Content)), Source: "unknown"}})
		return append(items, s.turnItem(harness.StateRunning, nil)), nil
	case "assistant":
		return s.assistant(m, id, at), nil
	case "tool":
		x, ok := s.tools[m.ToolCallID]
		if !ok {
			return nil, nil
		}
		delete(s.tools, m.ToolCallID)
		b := *x.Tool
		b.State, b.Ended = harness.StateOK, &at
		out, failed := "", false
		if m.ToolResult != nil {
			out, failed = hookstyle.ToolOutput(m.ToolResult.Output)
			if m.ToolResult.Cancelled {
				b.State = harness.StateCancelled
			}
		} else {
			out = hookstyle.Tail(text(m.Content), harness.MaxToolOutput)
		}
		if failed {
			b.State = harness.StateFailed
		}
		b.Output = out
		x.Tool = &b
		return []harness.Item{x}, nil
	}
	return nil, nil
}

func (s *session) assistant(m message, id string, at time.Time) []harness.Item {
	var items []harness.Item
	if r := text(m.Reasoning); r != "" {
		items = append(items, harness.Item{ID: id + ":thought", TurnID: s.turn, Kind: harness.KindThinking, At: at,
			Thinking: &harness.TextBody{Text: hookstyle.Clip(r)}})
	}
	body := text(m.Content)
	if body != "" {
		items = append(items, harness.Item{ID: id + ":text", TurnID: s.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: hookstyle.Clip(body)}})
	}
	for i, c := range m.ToolCalls {
		cid := c.ID
		if cid == "" {
			cid = id + ":call-" + strconv.Itoa(i)
		}
		args := json.RawMessage(c.Function.Arguments)
		if !json.Valid(args) {
			args = nil
		}
		title, cat, subj := hookstyle.ToolTitle(c.Function.Name, args)
		x := harness.Item{ID: cid, TurnID: s.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
			CallID: cid, Name: c.Function.Name, Title: hookstyle.Truncate(title, harness.MaxTitle), Category: cat,
			Subject: subj, State: harness.StateRunning, Started: at}}
		s.tools[cid] = x
		s.steps++
		items = append(items, x)
	}
	if s.turn == "" {
		return items
	}
	if len(m.ToolCalls) == 0 && body != "" {
		return append(items, s.close(at)...)
	}
	return append(items, s.turnItem(harness.StateRunning, nil))
}

func (s *session) close(at time.Time) []harness.Item {
	var items []harness.Item
	for id, x := range s.tools {
		b := *x.Tool
		b.State, b.Ended = harness.StateInterrupted, &at
		x.Tool = &b
		items = append(items, x)
		delete(s.tools, id)
	}
	items = append(items, s.turnItem(harness.StateOK, &at))
	s.turn = ""
	s.state["working"], s.state["idle"] = "false", "true"
	s.state[harness.StateWorkingAt] = harness.StampWorking(at)
	return items
}

func (s *session) turnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: s.turn, TurnID: s.turn, Kind: harness.KindTurn, At: s.started,
		Turn: &harness.TurnBody{State: state, Started: s.started, Ended: ended, Steps: s.steps}}
}

// text reads content given as a string or as a list of {type, text} parts.
func text(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n")
}
