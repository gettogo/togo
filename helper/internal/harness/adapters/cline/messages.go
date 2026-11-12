package cline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
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
	// lastAsking: the last document ends on a question nobody answered,
	// so Cline waits for the user and the turn is not over.
	lastAsking bool
	lastMsgs   []message
	tree       []int
	treeAt     time.Time
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
	// Pid is the Cline process that runs the session.
	Pid       int    `json:"pid"`
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

// owner says whether the session's Cline process is in this Togo session's
// process tree. Unknown when the manifest has no pid or the tree cannot be
// listed.
func (m *messages) owner(id string) source.Verdict {
	mf, ok := m.manifest(id)
	if !ok || mf.Pid <= 0 || m.env.Procs == nil {
		return source.Unknown
	}
	if time.Since(m.treeAt) > treeEvery {
		m.tree, m.treeAt = m.env.Procs.Tree(context.Background()), time.Now()
	}
	if len(m.tree) == 0 {
		return source.Unknown
	}
	if slices.Contains(m.tree, mf.Pid) {
		return source.Mine
	}
	return source.Foreign
}

// treeEvery bounds how often the process tree is listed.
const treeEvery = 2 * time.Second

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
		// DisplayRole "error" marks the message recordTerminalError
		// appends when a run fails (3.0.74): the error text, shown only.
		DisplayRole string `json:"displayRole"`
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
		// or a fresh start in the same pane. It must be this session's own
		// Cline process (the manifest names its pid), not one the user
		// started in a terminal; with no way to tell, the session followed
		// stays.
		if !m.belongs(d.SessionID, d.UpdatedAt) || (m.session != "" && d.UpdatedAt <= m.updated) {
			return nil, nil
		}
		switch m.owner(d.SessionID) {
		case source.Foreign:
			return nil, nil
		case source.Unknown:
			if m.session != "" {
				return nil, nil
			}
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
	if !m.lastOpen || m.lastTurn == "" || m.lastAsking {
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
	asks := map[string]int{}  // ask_question call id -> index in items
	closeTurn := func(state string, at time.Time) {
		if !open {
			return
		}
		// A question still open when its turn ends was never answered.
		for _, i := range asks {
			items[i] = answered(items[i], []string{})
		}
		asks = map[string]int{}
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
					if msg.Metadata.DisplayRole == "system" {
						// Cline's own note to the model, e.g. the
						// <hook_context> after a question was answered.
						continue
					}
					t, mode := userText(b.Text)
					if t != "" {
						text = append(text, t)
					}
					if mode != "" {
						m.state["mode"] = mode
					}
				case "tool_result":
					if i, ok := asks[b.ToolUseID]; ok {
						delete(asks, b.ToolUseID)
						items[i] = answered(items[i], askAnswer(b))
						continue
					}
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
			if msg.Metadata.DisplayRole == "error" {
				var text []string
				for _, b := range bs {
					if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
						text = append(text, strings.TrimSpace(b.Text))
					}
				}
				reason, line := errorText(strings.Join(text, "\n"))
				items = append(items, harness.Item{ID: id, TurnID: turn.ID, Kind: harness.KindNotice, At: at,
					Notice: harness.ErrorNotice(reason, line)})
				closeTurn(harness.StateFailed, at)
				continue
			}
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
					if b.Name == askTool {
						// A question, not a tool row; the turn waits on it.
						calls++
						asks[cid] = len(items)
						items = append(items, harness.Item{ID: cid, TurnID: turn.ID, Kind: harness.KindQuestion, At: at,
							Question: askBody(b.Input)})
						continue
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
	asking := len(asks) > 0
	if open {
		if at, ok := m.idleAt[turn.ID]; ok && !asking {
			closeTurn(harness.StateOK, at)
		} else {
			items = append(items, turn)
		}
	}
	m.lastTurn, m.lastOpen, m.lastAsking = turn.ID, open, asking
	return items
}
