package crush

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/turns"
)

// db maps rows of the project's .crush/crush.db, from two feeds: top-level
// sessions touched since the Togo session started (the newest is the one
// the TUI shows) and that session's messages. Crush rewrites a message row
// while it streams (its parts grow, updated_at moves) and marks it with a
// finish part when done; the same item ids are sent again as it grows.
type db struct {
	env harness.Env

	mu       sync.Mutex
	state    map[string]string
	sessions map[string]sessionRow
	done     map[string]bool // messages already mapped whole
	b        *turns.Builder
}

func newDB(env harness.Env) *db {
	return &db{env: env, state: map[string]string{}, sessions: map[string]sessionRow{},
		done: map[string]bool{}, b: turns.New()}
}

func (m *db) Name() string { return MapperDB }

func (m *db) State() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.state)+3)
	for k, v := range m.state {
		out[k] = v
	}
	m.b.State(out)
	return out
}

type sessionRow struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Created int64  `json:"created_at"`
	Updated int64  `json:"updated_at"`
}

type messageRow struct {
	ID       string `json:"id"`
	Session  string `json:"session_id"`
	Role     string `json:"role"`
	Parts    string `json:"parts"`
	Created  int64  `json:"created_at"`
	Updated  int64  `json:"updated_at"`
	Finished *int64 `json:"finished_at"`
	Summary  int    `json:"is_summary_message"`
}

// part is one entry of parts (Crush's message.ContentPart, {type, data}).
type part struct {
	Type string `json:"type"`
	Data struct {
		Text       string `json:"text"`
		Thinking   string `json:"thinking"`
		ID         string `json:"id"`
		Name       string `json:"name"`
		Input      string `json:"input"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
		IsError    bool   `json:"is_error"`
		Reason     string `json:"reason"`
		Message    string `json:"message"`
	} `json:"data"`
}

func (m *db) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var probe map[string]json.RawMessage
	if json.Unmarshal(rec.Data, &probe) != nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := probe["parts"]; ok {
		var r messageRow
		if json.Unmarshal(rec.Data, &r) != nil || r.ID == "" || r.Session != m.state["harness_session"] || m.done[r.ID] {
			return nil, nil
		}
		return m.message(r), nil
	}
	var s sessionRow
	if json.Unmarshal(rec.Data, &s) != nil || s.ID == "" {
		return nil, nil
	}
	m.sessions[s.ID] = s
	best := sessionRow{}
	for _, c := range m.sessions {
		if c.Updated > best.Updated || (c.Updated == best.Updated && c.Created > best.Created) {
			best = c
		}
	}
	if best.ID != m.state["harness_session"] {
		m.b.Reset()
		m.done = map[string]bool{}
		m.state["harness_session"] = best.ID
	}
	return nil, nil
}

func (m *db) message(r messageRow) []harness.Item {
	var parts []part
	if json.Unmarshal([]byte(r.Parts), &parts) != nil {
		return nil
	}
	at := time.Unix(r.Created, 0).UTC()
	finish := ""
	for _, p := range parts {
		if p.Type == "finish" {
			finish = p.Data.Reason
			if finish == "" {
				finish = "stop"
			}
		}
	}
	switch r.Role {
	case "user":
		m.done[r.ID] = true
		var text []string
		for _, p := range parts {
			if p.Type == "text" && p.Data.Text != "" {
				text = append(text, p.Data.Text)
			}
		}
		if len(text) == 0 {
			return nil
		}
		return m.b.User(r.ID, strings.Join(text, "\n"), at)
	case "tool":
		m.done[r.ID] = true
		var items []harness.Item
		for _, p := range parts {
			if p.Type != "tool_result" {
				continue
			}
			state := harness.StateOK
			if p.Data.IsError {
				state = harness.StateFailed
			}
			if x, ok := m.b.ToolEnd(p.Data.ToolCallID, p.Data.Content, state, at); ok {
				items = append(items, x)
			}
		}
		return items
	case "assistant":
		return m.assistant(r, parts, finish, at)
	}
	return nil
}

func (m *db) assistant(r messageRow, parts []part, finish string, at time.Time) []harness.Item {
	var items []harness.Item
	var text, thought []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Data.Text != "" {
				text = append(text, p.Data.Text)
			}
		case "reasoning":
			if t := strings.TrimSpace(p.Data.Thinking); t != "" {
				thought = append(thought, t)
			}
		case "tool_call":
			if p.Data.ID != "" {
				items = append(items, m.b.ToolStart(p.Data.ID, p.Data.Name, json.RawMessage(p.Data.Input), at))
			}
		}
	}
	if r.Summary == 1 {
		if finish == "" {
			return nil
		}
		m.done[r.ID] = true
		return []harness.Item{m.b.Notice(r.ID, harness.NoticeCompacted, "Conversation compacted", at)}
	}
	head := []harness.Item{}
	if len(thought) > 0 {
		head = append(head, m.b.Thinking(r.ID+":thought", strings.Join(thought, "\n"), at))
	}
	if len(text) > 0 {
		head = append(head, m.b.Assistant(r.ID+":text", strings.Join(text, ""), at))
	}
	items = append(head, items...)
	if finish != "" {
		m.done[r.ID] = true
	}
	if !m.b.Open() {
		return items
	}
	switch finish {
	case "", "tool_use":
		return append(items, m.b.TurnItem(harness.StateRunning, nil))
	case "canceled":
		return append(items, m.b.Close(harness.StateInterrupted, at)...)
	case "error", "permission_denied":
		return append(items, m.b.Close(harness.StateFailed, at)...)
	}
	return append(items, m.b.Close(harness.StateOK, at)...)
}
