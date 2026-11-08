package cline

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/wire"
)

// MapperMessages reads Cline's session messages file, as cline.yaml names it.
const MapperMessages = "cline.messages"

// startSlack is how much earlier than the Togo session a Cline session may
// say it started: both stamps can lose the fraction of a second.
const startSlack = 2 * time.Second

// maxManifest bounds a session manifest read.
const maxManifest = 256 << 10

var sessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// messages maps ~/.cline/data/sessions/<id>/<id>.messages.json, which
// Cline 3.0.74 rewrites whole after each assistant message and each loop
// iteration (persistSessionMessages). The layout was read from the
// installed CLI's bundled source: {version, updated_at, agent, sessionId,
// messages: [{id, role, content, ts, metadata, modelInfo, metrics}]},
// content a string or blocks of type text, thinking, redacted_thinking,
// image, file, tool_use {id, name, input} and tool_result {tool_use_id,
// name, content, is_error}. The manifest <id>.json next to it names the
// session's cwd, start, model and mode.
//
// Each document is mapped whole into items, and only the items that
// changed since the last document are returned. A user message opens a
// turn; an assistant message with text and no tool call closes it, and so
// does the session going idle by the pane (CloseIdle).
type messages struct {
	env     harness.Env
	created time.Time
	home    string

	mu       sync.Mutex
	state    map[string]string
	session  string
	updated  string
	sent     map[string]string // item id -> its JSON as last returned
	idleAt   map[string]time.Time
	lastTurn string
	lastOpen bool
	lastMsgs []message
}

func newMessages(env harness.Env) *messages {
	m := &messages{env: env, state: map[string]string{}, sent: map[string]string{}, idleAt: map[string]time.Time{},
		home: source.Expand("~", env.Vars[source.VarHome])}
	if c, err := time.Parse(time.RFC3339, env.Vars["created"]); err == nil {
		m.created = c
	}
	if id := argvID(env.Argv); id != "" {
		m.lock(id)
	}
	return m
}

func (m *messages) Name() string { return MapperMessages }

func (m *messages) State() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.state))
	for k, v := range m.state {
		out[k] = v
	}
	return out
}

// argvID is the session a `cline --id <id>` launch resumes.
func argvID(argv []string) string {
	for i, a := range argv {
		v, ok := strings.CutPrefix(a, "--id=")
		if !ok && a == "--id" && i+1 < len(argv) {
			v, ok = argv[i+1], true
		}
		if ok && sessionID.MatchString(v) {
			return v
		}
	}
	return ""
}

func (m *messages) dir(id string) string {
	return filepath.Join(m.home, ".cline", "data", "sessions", id)
}

func (m *messages) lock(id string) {
	m.session = id
	m.state["harness_session"] = id
	m.state["transcript_path"] = filepath.Join(m.dir(id), id+".messages.json")
	if mf, ok := m.manifest(id); ok {
		if mf.Model != "" {
			m.state["model"] = mf.Model
		}
		if mf.Metadata.Mode != "" {
			m.state["mode"] = mf.Metadata.Mode
		}
	}
}

type manifest struct {
	Cwd       string `json:"cwd"`
	StartedAt string `json:"started_at"`
	Model     string `json:"model"`
	Metadata  struct {
		Mode string `json:"mode"`
	} `json:"metadata"`
}

func (m *messages) manifest(id string) (manifest, bool) {
	var mf manifest
	p := filepath.Join(m.dir(id), id+".json")
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxManifest {
		return mf, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &mf) != nil {
		return mf, false
	}
	return mf, true
}

// belongs reports whether session id works in this session's folder and
// started, or was written to, since the Togo session began.
func (m *messages) belongs(id, updated string) bool {
	mf, ok := m.manifest(id)
	if !ok || !sameDir(mf.Cwd, m.env.Cwd) {
		return false
	}
	if m.created.IsZero() {
		return true
	}
	since := m.created.Add(-startSlack)
	for _, s := range []string{mf.StartedAt, updated} {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && !t.Before(since) {
			return true
		}
	}
	return false
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}

type document struct {
	UpdatedAt string    `json:"updated_at"`
	Agent     string    `json:"agent"`
	SessionID string    `json:"sessionId"`
	Messages  []message `json:"messages"`
}

type message struct {
	ID       string          `json:"id"`
	Role     string          `json:"role"`
	Content  json.RawMessage `json:"content"`
	TS       int64           `json:"ts"`
	Metadata struct {
		DisplayOnly bool `json:"displayOnly"`
	} `json:"metadata"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	_ = json.Unmarshal(raw, &bs)
	return bs
}

func (m *messages) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var d document
	if json.Unmarshal(rec.Data, &d) != nil || !sessionID.MatchString(d.SessionID) {
		return nil, nil
	}
	if d.Agent != "" && d.Agent != "lead" {
		return nil, nil // a subagent's or teammate's own file
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.SessionID != m.session {
		// A session of this folder newer than the one followed: a resume
		// or a fresh start in the same pane.
		if !m.belongs(d.SessionID, d.UpdatedAt) || (m.session != "" && d.UpdatedAt <= m.updated) {
			return nil, nil
		}
		m.lock(d.SessionID)
		m.lastMsgs, m.lastTurn, m.lastOpen = nil, "", false
	}
	m.updated = d.UpdatedAt
	m.lastMsgs = d.Messages
	return m.changed(m.render(d.Messages)), nil
}

// changed keeps the items whose content differs from what was returned.
func (m *messages) changed(items []harness.Item) []harness.Item {
	var out []harness.Item
	for _, it := range items {
		b, _ := json.Marshal(it)
		if m.sent[it.ID] == string(b) {
			continue
		}
		m.sent[it.ID] = string(b)
		out = append(out, it)
	}
	return out
}

// CloseIdle ends the open turn once the session went idle.
func (m *messages) CloseIdle(at time.Time) []harness.Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.lastOpen || m.lastTurn == "" {
		return nil
	}
	m.idleAt[m.lastTurn] = at.UTC()
	return m.changed(m.render(m.lastMsgs))
}

// render maps the whole conversation.
func (m *messages) render(msgs []message) []harness.Item {
	var items []harness.Item
	var turn harness.Item
	open := false
	tools := map[string]int{} // call id -> index in items
	closeTurn := func(state string, at time.Time) {
		if !open {
			return
		}
		for _, i := range tools {
			if t := items[i].Tool; t.State == harness.StateRunning {
				b := *t
				b.State, b.Ended = harness.StateInterrupted, &at
				items[i].Tool = &b
			}
		}
		tools = map[string]int{}
		tb := *turn.Turn
		tb.State, tb.Ended = state, &at
		turn.Turn = &tb
		items = append(items, turn)
		open = false
	}
	for n, msg := range msgs {
		at := time.UnixMilli(msg.TS).UTC()
		if msg.TS == 0 {
			at = m.created.UTC()
		}
		id := msg.ID
		if id == "" {
			id = "cline-" + strconv.Itoa(n)
		}
		bs := blocks(msg.Content)
		switch msg.Role {
		case "user":
			var text []string
			for _, b := range bs {
				switch b.Type {
				case "text":
					t, mode := userText(b.Text)
					if t != "" {
						text = append(text, t)
					}
					if mode != "" {
						m.state["mode"] = mode
					}
				case "tool_result":
					i, ok := tools[b.ToolUseID]
					if !ok {
						continue
					}
					delete(tools, b.ToolUseID)
					t := *items[i].Tool
					t.State, t.Ended = harness.StateOK, &at
					out, failed := resultText(b.Content)
					if b.IsError || failed {
						t.State = harness.StateFailed
					}
					t.Output = hookstyle.Tail(out, harness.MaxToolOutput)
					items[i].Tool = &t
				}
			}
			if len(text) == 0 {
				continue
			}
			closeTurn(harness.StateOK, at)
			tid := id + ".turn"
			turn = harness.Item{ID: tid, TurnID: tid, Kind: harness.KindTurn, At: at,
				Turn: &harness.TurnBody{State: harness.StateRunning, Started: at}}
			open = true
			items = append(items, harness.Item{ID: id, TurnID: tid, Kind: harness.KindUser, At: at,
				User: &harness.UserBody{Text: hookstyle.Clip(strings.Join(text, "\n\n")), Source: "unknown"}})
		case "assistant":
			calls, said := 0, false
			for i, b := range bs {
				bid := id + ":" + strconv.Itoa(i)
				switch b.Type {
				case "thinking":
					if strings.TrimSpace(b.Thinking) != "" {
						items = append(items, harness.Item{ID: bid, TurnID: turn.ID, Kind: harness.KindThinking, At: at,
							Thinking: &harness.TextBody{Text: hookstyle.Clip(b.Thinking)}})
					}
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						said = true
						items = append(items, harness.Item{ID: bid, TurnID: turn.ID, Kind: harness.KindAssistant, At: at,
							Assistant: &harness.TextBody{Text: hookstyle.Clip(b.Text)}})
					}
				case "tool_use":
					cid := b.ID
					if cid == "" {
						cid = bid
					}
					title, cat, subj := toolTitle(b.Name, b.Input)
					calls++
					if open {
						t := *turn.Turn
						t.Steps++
						turn.Turn = &t
					}
					tools[cid] = len(items)
					items = append(items, harness.Item{ID: cid, TurnID: turn.ID, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
						CallID: cid, Name: b.Name, Title: hookstyle.Truncate(title, harness.MaxTitle), Category: cat,
						Subject: subj, State: harness.StateRunning, Started: at}})
				}
			}
			if calls == 0 && said && !msg.Metadata.DisplayOnly {
				closeTurn(harness.StateOK, at)
			}
		}
	}
	if open {
		if at, ok := m.idleAt[turn.ID]; ok {
			closeTurn(harness.StateOK, at)
		} else {
			items = append(items, turn)
		}
	}
	m.lastTurn, m.lastOpen = turn.ID, open
	return items
}

// toolTitle names Cline's own tools (read_files, run_commands, editor,
// apply_patch, search_codebase, fetch_web_content) from their inputs, and
// any other tool the shared way.
func toolTitle(name string, input json.RawMessage) (string, string, *wire.Subject) {
	var in struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Commands json.RawMessage `json:"commands"`
		Path     string          `json:"path"`
		OldText  *string         `json:"old_text"`
		Input    string          `json:"input"`
		Queries  []string        `json:"queries"`
		Requests []struct {
			URL string `json:"url"`
		} `json:"requests"`
	}
	_ = json.Unmarshal(input, &in)
	more := func(first string, n int) string {
		if n > 1 {
			return first + " +" + strconv.Itoa(n-1)
		}
		return first
	}
	file := func(p string) *wire.Subject {
		return &wire.Subject{Kind: wire.SubjectFile, Text: hookstyle.Truncate(p, wire.MaxSubjectText)}
	}
	switch name {
	case "read_files":
		if len(in.Files) > 0 && in.Files[0].Path != "" {
			return "Read " + more(in.Files[0].Path, len(in.Files)), harness.ToolRead, file(in.Files[0].Path)
		}
	case "run_commands":
		var cmds []string
		if json.Unmarshal(in.Commands, &cmds) != nil {
			var one string
			if json.Unmarshal(in.Commands, &one) == nil && one != "" {
				cmds = []string{one}
			}
		}
		if len(cmds) > 0 {
			c := strings.Join(cmds, "\n")
			return strings.Join(cmds, "; "), harness.ToolShell, &wire.Subject{Kind: wire.SubjectCommand, Text: hookstyle.Truncate(c, wire.MaxSubjectText)}
		}
	case "editor":
		if in.Path != "" {
			if in.OldText == nil {
				return "Write " + in.Path, harness.ToolWrite, file(in.Path)
			}
			return "Edit " + in.Path, harness.ToolEdit, file(in.Path)
		}
	case "apply_patch":
		for _, l := range strings.Split(in.Input, "\n") {
			for _, p := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
				if f, ok := strings.CutPrefix(l, p); ok && strings.TrimSpace(f) != "" {
					f = strings.TrimSpace(f)
					return "Edit " + f, harness.ToolEdit, file(f)
				}
			}
		}
		return "Apply patch", harness.ToolEdit, nil
	case "search_codebase":
		if len(in.Queries) > 0 {
			return "Search " + more(in.Queries[0], len(in.Queries)), harness.ToolSearch, nil
		}
	case "fetch_web_content":
		if len(in.Requests) > 0 && in.Requests[0].URL != "" {
			u := in.Requests[0].URL
			return more(u, len(in.Requests)), harness.ToolWeb, &wire.Subject{Kind: wire.SubjectURL, Text: hookstyle.Truncate(u, wire.MaxSubjectText)}
		}
	}
	return hookstyle.ToolTitle(name, input)
}

var userInput = regexp.MustCompile(`(?s)^\s*<user_input(?: mode="([a-z]+)")?>(.*)</user_input>\s*$`)

// userText unwraps the <user_input mode="act"> envelope Cline stores a
// typed prompt in, returning the prompt and the mode it was sent in.
func userText(s string) (string, string) {
	if m := userInput.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[2]), m[1]
	}
	return strings.TrimSpace(s), ""
}

// resultText reads a tool result's content: a string (itself often the
// JSON of {query, result, success} or a list of them), a list of text
// parts, or else its JSON. failed is true when an entry says success false
// or carries an error.
func resultText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		t := strings.TrimSpace(s)
		if (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
			if out, failed := resultText(json.RawMessage(t)); out != "" {
				return out, failed
			}
		}
		return s, false
	}
	type entry struct {
		Text    string          `json:"text"`
		Result  json.RawMessage `json:"result"`
		Error   string          `json:"error"`
		Success *bool           `json:"success"`
	}
	var list []entry
	if json.Unmarshal(raw, &list) != nil {
		var one entry
		if json.Unmarshal(raw, &one) == nil && (one.Text != "" || len(one.Result) > 0 || one.Error != "") {
			list = []entry{one}
		}
	}
	var out []string
	failed := false
	for _, p := range list {
		if p.Error != "" || (p.Success != nil && !*p.Success) {
			failed = true
		}
		switch {
		case p.Text != "":
			out = append(out, p.Text)
		case len(p.Result) > 0:
			r, f := resultText(p.Result)
			failed = failed || f
			out = append(out, r)
		case p.Error != "":
			out = append(out, p.Error)
		}
	}
	if len(out) > 0 {
		return strings.Join(out, "\n"), failed
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String(), false
	}
	return string(raw), false
}
