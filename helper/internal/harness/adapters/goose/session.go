package goose

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/turns"
)

// db maps rows of Goose's sessions.db, from two feeds: sessions rows (one
// per session touched since the Togo session started) pick the session
// working in this folder and give its id and mode; messages rows of that
// session become the chat. Goose writes a message once it is whole: a
// reply appears when it ends, a tool call when the model asked for it.
type db struct {
	env harness.Env

	mu       sync.Mutex
	state    map[string]string
	sessions map[string]sessionRow
	asks     map[string]harness.Item        // confirmations and elicitations by item id
	early    map[string]map[string][]string // elicitation answers stored before their request
	b        *turns.Builder
}

func newDB(env harness.Env) *db {
	return &db{env: env, state: map[string]string{}, sessions: map[string]sessionRow{}, asks: map[string]harness.Item{},
		early: map[string]map[string][]string{}, b: turns.New()}
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
	ID         string `json:"id"`
	WorkingDir string `json:"working_dir"`
	Mode       string `json:"goose_mode"`
	Name       string `json:"name"`
	Updated    string `json:"updated_at"`
	Created    string `json:"created_at"`
}

type messageRow struct {
	ID       int64           `json:"id"`
	MsgID    string          `json:"message_id"`
	Session  string          `json:"session_id"`
	Role     string          `json:"role"`
	Content  string          `json:"content_json"`
	Created  int64           `json:"created_timestamp"`
	Metadata json.RawMessage `json:"metadata_json"`
}

// part is one entry of content_json (Goose's MessageContent, tagged by type).
type part struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
	ID       string `json:"id"`
	ToolCall *struct {
		Status string `json:"status"`
		Value  struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"value"`
		Error string `json:"error"`
	} `json:"toolCall"`
	ToolResult *struct {
		Status string `json:"status"`
		Value  struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"value"`
		Error string `json:"error"`
	} `json:"toolResult"`
	Msg string `json:"msg"`
	// An error part (MessageContent::Error, 1.57): kind is authentication,
	// contextLengthExceeded or other, message Goose's own words.
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// A systemNotification part names its kind in notificationType.
	NotificationType string `json:"notificationType"`
	// An actionRequired part (question.go) carries its request or response.
	Data json.RawMessage `json:"data"`
}

func (m *db) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var probe map[string]json.RawMessage
	if json.Unmarshal(rec.Data, &probe) != nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := probe["content_json"]; ok {
		var r messageRow
		if json.Unmarshal(rec.Data, &r) != nil || r.Session != m.state["harness_session"] {
			return nil, nil
		}
		return m.message(r), nil
	}
	var s sessionRow
	if json.Unmarshal(rec.Data, &s) != nil || s.ID == "" {
		return nil, nil
	}
	m.session(s)
	return nil, nil
}

// session keeps the newest-updated session in this folder. When it changes
// (a resume), the messages feed restarts on the new id and replays it.
func (m *db) session(s sessionRow) {
	if !sameDir(s.WorkingDir, m.env.Cwd) {
		return
	}
	m.sessions[s.ID] = s
	best := sessionRow{}
	for _, c := range m.sessions {
		if c.Updated > best.Updated || (c.Updated == best.Updated && c.ID > best.ID) {
			best = c
		}
	}
	if best.ID != m.state["harness_session"] {
		m.b.Reset()
		m.asks, m.early = map[string]harness.Item{}, map[string]map[string][]string{}
		m.state["harness_session"] = best.ID
	}
	if best.Mode != "" {
		m.state["mode"] = best.Mode
	}
}

func (m *db) message(r messageRow) []harness.Item {
	var meta struct {
		UserVisible *bool `json:"userVisible"`
	}
	if len(r.Metadata) > 0 {
		var s string
		if json.Unmarshal(r.Metadata, &s) == nil {
			json.Unmarshal([]byte(s), &meta)
		}
	}
	var parts []part
	if json.Unmarshal([]byte(r.Content), &parts) != nil {
		return nil
	}
	at := time.Unix(r.Created, 0).UTC()
	items := m.actions(parts, at) // answers are stored hidden
	if meta.UserVisible != nil && !*meta.UserVisible {
		return items // turn context and other agent-only messages
	}
	id := r.MsgID
	if id == "" {
		id = "goose-" + strconv.FormatInt(r.ID, 10)
	}
	switch r.Role {
	case "user":
		return append(items, m.user(id, parts, at)...)
	case "assistant":
		return append(items, m.assistant(id, parts, at)...)
	}
	return items
}

func (m *db) user(id string, parts []part, at time.Time) []harness.Item {
	var items []harness.Item
	var text []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				text = append(text, p.Text)
			}
		case "toolResponse":
			out, state := toolOutput(p)
			if x, ok := m.b.ToolEnd(p.ID, out, state, at); ok {
				items = append(items, x)
			}
		}
	}
	if len(text) > 0 {
		items = append(items, m.closeOpen()...)
		items = append(items, m.b.User(id, strings.Join(text, "\n"), at)...)
	}
	return items
}

func (m *db) assistant(id string, parts []part, at time.Time) []harness.Item {
	var items []harness.Item
	calls, reply, failed := 0, false, false
	for i, p := range parts {
		pid := id + ":" + strconv.Itoa(i)
		switch p.Type {
		case "thinking":
			if p.Thinking != "" {
				items = append(items, m.b.Thinking(pid, p.Thinking, at))
			}
		case "text":
			if p.Text != "" {
				items = append(items, m.b.Assistant(pid, p.Text, at))
				reply = true
			}
		case "toolRequest", "frontendToolRequest":
			if p.ToolCall == nil || p.ID == "" {
				continue
			}
			calls++
			x := m.b.ToolStart(p.ID, p.ToolCall.Value.Name, p.ToolCall.Value.Arguments, at)
			if p.ToolCall.Status == "error" {
				x, _ = m.b.ToolEnd(p.ID, p.ToolCall.Error, harness.StateFailed, at)
			}
			items = append(items, x)
		case "conversationCompacted", "summarizationRequested":
			items = append(items, m.b.Notice(pid, harness.NoticeCompacted, "Conversation compacted", at))
		case "contextLengthExceeded":
			items = append(items, m.errorItem(pid, harness.ErrContextOverflow, firstNonEmpty(p.Msg, "Context length exceeded"), at))
			failed = true
		case "error":
			reason, text := errorReason(p.Kind, p.Message)
			items = append(items, m.errorItem(pid, reason, text, at))
			failed = true
		case "systemNotification":
			if p.NotificationType == "creditsExhausted" {
				items = append(items, m.errorItem(pid, harness.ErrRateLimit, firstNonEmpty(p.Msg, "Out of credits"), at))
				failed = true
			}
		}
	}
	if !m.b.Open() {
		return items
	}
	if failed {
		// Goose ends the turn after an error part: it waits for a resend.
		items = append(items, m.closeOpen()...)
		return append(items, m.b.Close(harness.StateFailed, at)...)
	}
	if calls == 0 && reply {
		return append(items, m.b.Close(harness.StateOK, at)...)
	}
	return append(items, m.b.TurnItem(harness.StateRunning, nil))
}

func (m *db) errorItem(id, reason, text string, at time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: m.b.TurnID(), Kind: harness.KindNotice, At: at,
		Notice: harness.ErrorNotice(reason, humanError(reason, text))}
}

// errorReason reads an error part's kind and message. Goose wraps provider
// failures as "Ran into this error: …" and adds a paragraph asking for a
// retry or resend; only the first paragraph, without the wrapper, is kept.
func errorReason(kind, msg string) (string, string) {
	text := strings.TrimSpace(msg)
	if i := strings.Index(text, "\n\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	text = strings.TrimPrefix(text, "Ran into this error: ")
	text = strings.TrimSuffix(strings.TrimSpace(text), ".")
	switch kind {
	case "authentication":
		return harness.ErrAuth, text
	case "contextLengthExceeded":
		return harness.ErrContextOverflow, text
	}
	if notFound.MatchString(text) {
		return harness.ErrModelNotFound, text
	}
	return harness.ClassifyError(text), text
}

// notFound is an OpenAI-compatible 404 about the model ("Resource not
// found (404) at …: no router for requested model").
var notFound = regexp.MustCompile(`(?i)\(404\).*\bmodel\b|\bmodel\b.*\(404\)`)

// humanError leads the agent's words with what they mean for the user.
func humanError(reason, text string) string {
	lead := map[string]string{
		harness.ErrAuth:                "The provider refused the API key or sign-in.",
		harness.ErrProviderUnreachable: "Can't reach the model provider.",
		harness.ErrRateLimit:           "Rate limit or quota reached.",
		harness.ErrModelNotFound:       "The provider doesn't have this model.",
		harness.ErrContextOverflow:     "The conversation is too long for the model.",
	}[reason]
	switch {
	case lead == "":
		return text
	case text == "":
		return lead
	}
	return lead + " " + text
}

func toolOutput(p part) (string, string) {
	if p.ToolResult == nil {
		return "", harness.StateOK
	}
	if p.ToolResult.Status == "error" {
		return p.ToolResult.Error, harness.StateFailed
	}
	var out []string
	for _, c := range p.ToolResult.Value.Content {
		if c.Text != "" {
			out = append(out, c.Text)
		}
	}
	state := harness.StateOK
	if p.ToolResult.Value.IsError {
		state = harness.StateFailed
	}
	return strings.Join(out, "\n"), state
}

// sameDir compares folders after resolving links: Goose stores the
// resolved path (/private/tmp/...), the session may name /tmp/....
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return resolve(a) == resolve(b)
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
