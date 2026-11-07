package opencode

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
)

// MapperEvents2 maps OpenCode 2.x's /api/event stream.
const MapperEvents2 = "opencode2.events"

// Events2 maps the event stream of the private `opencode serve` Togo starts
// for a 2.x session (checked on 2.2.5). Each event is {id, created, type,
// location, data}. A turn is one execution (session.execution.started to
// .succeeded, .failed or .interrupted); prompts arrive as inbox items, the
// answer as steps of streamed text, reasoning and tool calls, and questions
// and permissions as form.created and permission.asked.
//
// The server serves only this TUI, so the mapper follows the session the
// last user prompt went to (the TUI's, or the one --session names) and
// the child sessions it starts.
type Events2 struct {
	env harness.Env

	mu       sync.Mutex
	state    map[string]string
	main     string
	children map[string]*session // child session id -> its agent
	texts    map[string]*part    // streamed text and reasoning by item id
	tools    map[string]harness.Item
	inputs   map[string]json.RawMessage // tool input by call id
	turns    map[string]*turn
	turnOf   map[string]string // session -> its running turn
	inbox    map[string]string // undelivered user prompt per session
	open     map[string]harness.Item
}

// NewEvents2 returns a 2.x mapper for one session.
func NewEvents2(env harness.Env) *Events2 {
	e := &Events2{env: env, state: map[string]string{"working": "false", "idle": "true"},
		children: map[string]*session{}, texts: map[string]*part{}, tools: map[string]harness.Item{}, inputs: map[string]json.RawMessage{},
		turns: map[string]*turn{}, turnOf: map[string]string{}, inbox: map[string]string{}, open: map[string]harness.Item{}}
	if id := sessionFlag(env.Argv); id != "" {
		e.setMain(id)
	}
	return e
}

func (e *Events2) Name() string { return MapperEvents2 }

// State reports harness_session, model, model_ref, provider, model_id,
// mode (the agent), turn, working and idle.
func (e *Events2) State() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.state))
	for k, v := range e.state {
		out[k] = v
	}
	return out
}

// sessionFlag reads --session/-s from the TUI's argv: a resumed session is
// known before its first prompt.
func sessionFlag(argv []string) string {
	for i, a := range argv {
		if v, ok := strings.CutPrefix(a, "--session="); ok {
			return v
		}
		if (a == "--session" || a == "-s") && i+1 < len(argv) && strings.HasPrefix(argv[i+1], "ses") {
			return argv[i+1]
		}
	}
	return ""
}

type event2 struct {
	ID      string          `json:"id"`
	Created int64           `json:"created"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
}

// ev2 is the union of the data fields the mapper reads.
type ev2 struct {
	SessionID   string          `json:"sessionID"`
	ParentID    string          `json:"parentID"`
	Title       string          `json:"title"`
	Agent       string          `json:"agent"`
	Model       *modelRef2      `json:"model"`
	MessageID   string          `json:"assistantMessageID"`
	Ordinal     int             `json:"ordinal"`
	Delta       string          `json:"delta"`
	Text        string          `json:"text"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Input       json.RawMessage `json:"input"`
	Content     []content2      `json:"content"`
	Metadata    json.RawMessage `json:"metadata"`
	Error       *error2         `json:"error"`
	InboxID     string          `json:"inboxID"`
	Item        *inboxItem2     `json:"item"`
	Reason      string          `json:"reason"`
	Attempt     int             `json:"attempt"`
	RequestID   string          `json:"requestID"`
	Reply       string          `json:"reply"`
	Action      string          `json:"action"`
	Resources   []string        `json:"resources"`
	Source      *source2        `json:"source"`
	Form        *form2          `json:"form"`
	Answer      map[string]any  `json:"answer"`
	ContextUsed *int            `json:"contextUsed"`
}

type modelRef2 struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

type content2 struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type error2 struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type inboxItem2 struct {
	Type    string `json:"type"`
	Payload struct {
		Text string `json:"text"`
	} `json:"payload"`
}

type source2 struct {
	Type      string `json:"type"`
	MessageID string `json:"messageID"`
	ID        string `json:"id"`
}

func (e *Events2) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var ev event2
	if json.Unmarshal(rec.Data, &ev) != nil || ev.Type == "" {
		return nil, nil
	}
	at := rec.At
	if ev.Created > 0 {
		at = ms(ev.Created)
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	var d ev2
	if len(ev.Data) > 0 && json.Unmarshal(ev.Data, &d) != nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch ev.Type {
	case httpsrc.Reconnected:
		return e.dropOpen()
	case "session.created":
		return e.created(d, at)
	case "session.inbox.enqueued":
		return e.enqueued(d, at)
	case "session.execution.started":
		return e.started(ev.ID, d, at)
	case "session.execution.succeeded":
		return e.ended(d.SessionID, harness.StateOK, at)
	case "session.execution.failed":
		items, ops := e.ended(d.SessionID, harness.StateFailed, at)
		if d.Error != nil && e.follows(d.SessionID) {
			items = append(items, e.notice("failed-"+ev.ID, e.turnFor(d.SessionID), d.SessionID, at, "error", harness.NoticeError, errText(d.Error)))
		}
		return items, ops
	case "session.execution.interrupted":
		if !e.follows(d.SessionID) {
			return nil, nil
		}
		t := e.turnFor(d.SessionID)
		items, ops := e.ended(d.SessionID, harness.StateInterrupted, at)
		return append(items, e.notice("interrupted-"+ev.ID, t, d.SessionID, at, "info", harness.NoticeInterrupted, "Interrupted")), ops
	case "session.step.started":
		if d.SessionID == e.main {
			e.setAgentModel(d.Agent, d.Model)
		}
	case "session.agent.selected", "session.model.selected":
		if d.SessionID == e.main || e.main == "" {
			e.setAgentModel(d.Agent, d.Model)
		}
	case "session.text.started", "session.reasoning.started":
		e.textStarted(ev.Type, d, at)
	case "session.text.delta", "session.reasoning.delta":
		return e.textDelta(ev.Type, d, at)
	case "session.text.ended", "session.reasoning.ended":
		return e.textEnded(ev.Type, d, at)
	case "session.tool.input.started", "session.tool.called", "session.tool.progress",
		"session.tool.success", "session.tool.failed":
		return e.tool(ev.Type, d, at)
	case "session.step.failed":
		if d.Error != nil && d.Error.Type != "aborted" && e.follows(d.SessionID) {
			return []harness.Item{e.notice("step-failed-"+d.MessageID, e.turnFor(d.SessionID), d.SessionID, at, "error", harness.NoticeError, errText(d.Error))}, nil
		}
	case "session.retry.scheduled":
		if e.follows(d.SessionID) {
			text := "Retrying"
			if d.Error != nil && d.Error.Message != "" {
				text = d.Error.Message
			}
			return []harness.Item{e.notice("retry-"+ev.ID, e.turnFor(d.SessionID), d.SessionID, at, "warning", harness.NoticeRateLimit, text)}, nil
		}
	case "session.compaction.ended":
		if e.follows(d.SessionID) {
			return []harness.Item{e.notice("compacted-"+ev.ID, e.turnFor(d.SessionID), d.SessionID, at, "info", harness.NoticeCompacted, "Context compacted")}, nil
		}
	case "permission.asked":
		return e.permission2(d, at)
	case "permission.replied":
		return e.permissionReplied(d)
	case "form.created":
		return e.form(d, at)
	case "form.replied", "form.cancelled":
		return e.formDone(ev.Type, d)
	}
	return nil, nil
}

func (e *Events2) setMain(id string) {
	e.main = id
	e.state["harness_session"] = id
}

func (e *Events2) setAgentModel(agent string, m *modelRef2) {
	if agent != "" {
		e.state["mode"] = agent
	}
	if m != nil && m.ID != "" {
		e.state["model"] = m.ProviderID + "/" + m.ID
		e.state["provider"], e.state["model_id"] = m.ProviderID, m.ID
		e.state["model_ref"] = modelRef(m.ProviderID, m.ID)
	}
}

// follows reports whether a session's events belong to this chat.
func (e *Events2) follows(sess string) bool {
	return sess != "" && (sess == e.main || e.children[sess] != nil)
}

// parent is the agent item a child session's items hang under.
func (e *Events2) parent(sess string) string {
	if c := e.children[sess]; c != nil {
		return c.parent
	}
	return ""
}

func (e *Events2) created(d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if d.ParentID == "" || !e.follows(d.ParentID) {
		return nil, nil
	}
	c := &session{parent: "agent-" + d.SessionID, title: d.Title}
	e.children[d.SessionID] = c
	name := d.Title
	if name == "" {
		name = "Subagent"
	}
	x := harness.Item{ID: c.parent, Parent: e.parent(d.ParentID), Kind: harness.KindAgent, At: at,
		Agent: &harness.AgentBody{AgentID: d.SessionID, Name: truncate(name, harness.MaxTitle), Role: d.Agent, State: harness.StateRunning, Started: at}}
	c.agent = x
	return []harness.Item{x}, []harness.Op{agentRow(x)}
}

// enqueued shows a user prompt. A prompt in another top-level session
// means the TUI moved there (a new session, a resumed one).
func (e *Events2) enqueued(d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if d.Item == nil || d.Item.Type != "user" || d.InboxID == "" {
		return nil, nil
	}
	if d.SessionID != e.main && e.children[d.SessionID] == nil {
		e.setMain(d.SessionID)
	}
	turn := e.turnOf[d.SessionID]
	if turn == "" {
		turn = "turn-" + d.InboxID
		e.inbox[d.SessionID] = turn
	}
	x := textItem(d.InboxID, turn, e.parent(d.SessionID), harness.KindUser, at, d.Item.Payload.Text, false)
	return []harness.Item{x}, nil
}

// started opens a turn, named after the prompt that started it (an item
// id of its own: "turn-" and the prompt's).
func (e *Events2) started(evID string, d ev2, at time.Time) ([]harness.Item, []harness.Op) {
	if !e.follows(d.SessionID) {
		if e.main != "" || d.SessionID == "" {
			return nil, nil
		}
		e.setMain(d.SessionID)
	}
	id := e.inbox[d.SessionID]
	delete(e.inbox, d.SessionID)
	if id == "" {
		id = "turn-" + evID
	}
	tu := &turn{session: d.SessionID, started: at, state: harness.StateRunning, steps: map[string]bool{}, files: map[string]bool{}}
	e.turns[id] = tu
	e.turnOf[d.SessionID] = id
	if d.SessionID == e.main {
		e.state["turn"] = id
		e.state["working"], e.state["idle"] = "true", "false"
	}
	return []harness.Item{e.turnItem(id, tu, nil)}, nil
}

func (e *Events2) turnFor(sess string) string {
	if t := e.turnOf[sess]; t != "" {
		return t
	}
	if sess == e.main {
		return e.state["turn"]
	}
	return ""
}

// ended closes the session's turn; a child session ending ends its agent.
func (e *Events2) ended(sess, state string, at time.Time) ([]harness.Item, []harness.Op) {
	if !e.follows(sess) {
		return nil, nil
	}
	if sess == e.main {
		e.state["working"], e.state["idle"] = "false", "true"
	}
	var items []harness.Item
	var ops []harness.Op
	if id := e.turnOf[sess]; id != "" {
		delete(e.turnOf, sess)
		if tu := e.turns[id]; tu != nil {
			tu.state = state
			items = append(items, e.turnItem(id, tu, ptr(at)))
			delete(e.turns, id)
		}
	}
	if c := e.children[sess]; c != nil && c.agent.Agent != nil && c.agent.Agent.State == harness.StateRunning {
		x := c.agent
		body := *x.Agent
		body.State, body.Ended = state, ptr(at)
		x.Agent = &body
		c.agent = x
		items = append(items, x)
		ops = append(ops, agentRow(x))
	}
	return items, ops
}

func (e *Events2) turnItem(id string, tu *turn, ended *time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: id, Parent: e.parent(tu.session), Kind: harness.KindTurn, At: tu.started,
		Turn: &harness.TurnBody{State: tu.state, Started: tu.started, Ended: ended, Steps: len(tu.steps), FilesChanged: len(tu.files)}}
}

func (e *Events2) notice(id, turn, sess string, at time.Time, level, code, text string) harness.Item {
	return harness.Item{ID: id, TurnID: turn, Parent: e.parent(sess), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: truncate(text, 400)}}
}

func errText(err *error2) string {
	if err.Message != "" {
		return err.Message
	}
	return err.Type
}

// dropOpen closes open questions after the stream reconnected (see
// Events.dropOpen).
func (e *Events2) dropOpen() ([]harness.Item, []harness.Op) {
	ids := make([]string, 0, len(e.open))
	for id := range e.open {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var items []harness.Item
	var ops []harness.Op
	for _, id := range ids {
		x := closeQuestion(e.open[id], nil)
		delete(e.open, id)
		items = append(items, x)
		ops = append(ops, harness.Op{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: id})
	}
	return items, ops
}
