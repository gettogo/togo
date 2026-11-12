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
	done     map[string]bool  // messages already mapped whole
	asks     map[string]asked // question tool calls by call id
	b        *turns.Builder
	// pin is a session id this Togo session's own relaunch asked for; it is
	// the one switch allowed once the chat has content. shown is set when
	// the followed session's messages were mapped.
	pin   string
	shown bool
}

func newDB(env harness.Env) *db {
	return &db{env: env, pin: sessionArg(env.Argv), state: map[string]string{}, sessions: map[string]sessionRow{},
		done: map[string]bool{}, asks: map[string]asked{}, b: turns.New()}
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
		Details    string `json:"details"`
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
		m.shown = true
		return m.message(r), nil
	}
	var s sessionRow
	if json.Unmarshal(rec.Data, &s) != nil || s.ID == "" {
		return nil, nil
	}
	m.session(s)
	return nil, nil
}

// session keeps the newest-updated session until it has content. Crush's
// database names no process, so a session another Crush creates in the
// same project cannot be told from this one's: once the chat has content
// it stays on its session, and only this Togo session's own relaunch
// (--session, see Relaunched) moves it.
func (m *db) session(s sessionRow) {
	m.sessions[s.ID] = s
	best := sessionRow{}
	for _, c := range m.sessions {
		if c.Updated > best.Updated || (c.Updated == best.Updated && c.Created > best.Created) {
			best = c
		}
	}
	if m.pin != "" {
		p, ok := m.sessions[m.pin]
		if !ok {
			return
		}
		best = p
	} else if m.shown && m.state["harness_session"] != "" {
		best = m.sessions[m.state["harness_session"]]
	}
	if best.ID != m.state["harness_session"] {
		m.b.Reset()
		m.done = map[string]bool{}
		m.asks = map[string]asked{}
		m.state["harness_session"] = best.ID
		m.shown = false
	}
	if m.pin != "" && best.ID == m.pin {
		m.pin = ""
	}
}

// Relaunched learns the session a relaunch resumes (`--session <id>`).
func (m *db) Relaunched(argv []string, _ time.Time) []harness.Op {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id := sessionArg(argv); id != "" {
		m.pin = id
		if r, ok := m.sessions[id]; ok {
			m.session(r)
		}
	}
	return nil
}

// sessionArg is the id a `--session <id>`, `-s <id>` or `--session=<id>`
// names.
func sessionArg(argv []string) string {
	for i, a := range argv {
		if v, ok := strings.CutPrefix(a, "--session="); ok {
			return v
		}
		if (a == "--session" || a == "-s") && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

func (m *db) message(r messageRow) []harness.Item {
	var parts []part
	if json.Unmarshal([]byte(r.Parts), &parts) != nil {
		return nil
	}
	at := time.Unix(r.Created, 0).UTC()
	finish := ""
	var fin part
	for _, p := range parts {
		if p.Type == "finish" {
			fin = p
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
			if x, ok := m.answered(p.Data.ToolCallID, p.Data.Content, p.Data.IsError); ok {
				items = append(items, x)
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
		return m.assistant(r, parts, fin, at)
	}
	return nil
}

func (m *db) assistant(r messageRow, parts []part, fin part, at time.Time) []harness.Item {
	finish := fin.Data.Reason
	if fin.Type == "finish" && finish == "" {
		finish = "stop"
	}
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
			if p.Data.ID == "" {
				continue
			}
			if p.Data.Name == questionTool {
				if q, ok := m.question(p.Data.ID, p.Data.Input, at); ok {
					items = append(items, q)
					continue
				}
			}
			items = append(items, m.b.ToolStart(p.Data.ID, p.Data.Name, json.RawMessage(p.Data.Input), at))
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
	if finish == "error" {
		items = append(items, harness.Item{ID: r.ID + ":error", TurnID: m.b.TurnID(), Kind: harness.KindNotice,
			At: at, Notice: errorNotice(fin.Data.Message, fin.Data.Details)})
	}
	if !m.b.Open() {
		return items
	}
	switch finish {
	case "", "tool_use":
		return append(items, m.b.TurnItem(harness.StateRunning, nil))
	case "canceled":
		items = append(items, m.closeOpen()...)
		return append(items, m.b.Close(harness.StateInterrupted, at)...)
	case "error", "permission_denied":
		items = append(items, m.closeOpen()...)
		return append(items, m.b.Close(harness.StateFailed, at)...)
	}
	return append(items, m.b.Close(harness.StateOK, at)...)
}
