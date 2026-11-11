package opencode

import (
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
)

// Events maps OpenCode's /event stream. The server serves every session of
// its project; the mapper follows the TUI's session (the main one: the one
// the user last prompted or selected) and its subagent sessions, whose items
// go into the subagent's own conversation.
type Events struct {
	env harness.Env

	mu        sync.Mutex
	state     map[string]string
	main      string
	sessions  map[string]*session
	messages  map[string]message
	parts     map[string]*part
	turns     map[string]*turn
	open      map[string]harness.Item // questions and permissions not yet answered
	tools     map[string]harness.Item // tool items by call id, for permission waits
	planFirst map[string]time.Time
}

type session struct {
	parent string // agent item id of a subagent session
	title  string
	agent  harness.Item
}

type message struct {
	role    string
	session string
	turn    string
}

type part struct {
	kind  string
	turn  string
	sess  string
	text  string
	start time.Time
	last  time.Time
}

type turn struct {
	session string
	started time.Time
	state   string
	steps   map[string]bool
	files   map[string]bool
}

// NewEvents returns a mapper for one session.
func NewEvents(env harness.Env) *Events {
	return &Events{env: env, state: map[string]string{"working": "false", "idle": "true"},
		sessions: map[string]*session{}, messages: map[string]message{}, parts: map[string]*part{},
		turns: map[string]*turn{}, open: map[string]harness.Item{}, tools: map[string]harness.Item{},
		planFirst: map[string]time.Time{}}
}

func (e *Events) Name() string { return MapperEvents }

// State reports harness_session, model ("provider/model"), model_ref (the
// picker's id), provider, model_id, mode (the agent), turn, working, idle.
func (e *Events) State() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.state))
	for k, v := range e.state {
		out[k] = v
	}
	return out
}

type event struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

func (e *Events) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var ev event
	if json.Unmarshal(rec.Data, &ev) != nil || ev.Type == "" {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	e.mu.Lock()
	defer e.mu.Unlock()
	p := ev.Properties
	switch ev.Type {
	case "session.created", "session.updated":
		return e.sessionInfo(p, at)
	case "tui.session.select":
		var s struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(p, &s) == nil && s.SessionID != "" {
			e.setMain(s.SessionID)
		}
	case "message.updated":
		return e.messageInfo(p, at)
	case "message.part.updated":
		return e.partUpdated(p, at)
	case "message.part.delta":
		return e.partDelta(p, at)
	case "session.status":
		return e.sessionStatus(p, at)
	case "session.idle":
		var s struct {
			SessionID string `json:"sessionID"`
		}
		json.Unmarshal(p, &s)
		return e.idle(s.SessionID, at)
	case "session.diff":
		return nil, e.diff(p)
	case "todo.updated":
		return e.todos(p, at)
	case "question.asked":
		return e.question(p, at)
	case "question.replied", "question.rejected", "permission.replied":
		return e.answered(ev.Type, p)
	case "permission.asked":
		return e.permission(p, at)
	case httpsrc.Reconnected:
		e.lostTrack()
		return e.dropOpen()
	case "session.compacted":
		var s struct {
			SessionID string `json:"sessionID"`
		}
		json.Unmarshal(p, &s)
		if s.SessionID == e.main {
			return []harness.Item{e.notice("compacted-"+strconv.FormatInt(at.UnixMilli(), 10), e.state["turn"], "", at, "info", harness.NoticeCompacted, "Context compacted")}, nil
		}
	case "session.error":
		return e.sessionError(p, at), nil
	}
	return nil, nil
}

func (e *Events) setMain(id string) {
	e.main = id
	e.state["harness_session"] = id
}

func (e *Events) parentOf(sess string) string {
	if s := e.sessions[sess]; s != nil {
		return s.parent
	}
	return ""
}

// follows reports whether a session's events belong to this conversation.
func (e *Events) follows(sess string) bool {
	return sess != "" && (sess == e.main || e.parentOf(sess) != "")
}

type sessionPayload struct {
	Info struct {
		ID       string `json:"id"`
		ParentID string `json:"parentID"`
		Title    string `json:"title"`
		Agent    string `json:"agent"`
		Model    *struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
		} `json:"model"`
		Time struct {
			Created int64 `json:"created"`
		} `json:"time"`
	} `json:"info"`
}

func (e *Events) sessionInfo(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p sessionPayload
	if json.Unmarshal(raw, &p) != nil || p.Info.ID == "" {
		return nil, nil
	}
	in := p.Info
	if in.ParentID == "" {
		if e.main == "" {
			e.setMain(in.ID)
		}
		if in.ID == e.main {
			if in.Model != nil && in.Model.ID != "" {
				e.setModel(in.Model.ProviderID, in.Model.ID)
			}
			if in.Agent != "" {
				e.state["mode"] = in.Agent
			}
		}
		return nil, nil
	}
	if !e.follows(in.ParentID) {
		return nil, nil
	}
	s := e.sessions[in.ID]
	if s == nil {
		s = &session{parent: "agent-" + in.ID}
		e.sessions[in.ID] = s
	}
	if in.Title == s.title {
		return nil, nil
	}
	s.title = in.Title
	name := in.Title
	if name == "" {
		name = "Subagent"
	}
	started := orAt(ms(in.Time.Created), at)
	x := harness.Item{ID: s.parent, Parent: e.parentOf(in.ParentID), Kind: harness.KindAgent, At: started,
		Agent: &harness.AgentBody{AgentID: in.ID, Name: truncate(name, harness.MaxTitle), Role: in.Agent, State: harness.StateRunning, Started: started}}
	if s.agent.Agent != nil {
		x.At, x.Agent.Started, x.Agent.State = s.agent.At, s.agent.Agent.Started, s.agent.Agent.State
	}
	s.agent = x
	return []harness.Item{x}, []harness.Op{agentRow(x)}
}

func agentRow(x harness.Item) harness.Op {
	row := harness.Row{ID: x.Agent.AgentID, Title: x.Agent.Name, Subtitle: x.Agent.Role, State: x.Agent.State,
		Started: ptr(x.Agent.Started), Open: &harness.Open{Kind: harness.OpenChat, Ref: x.ID},
		Actions: []harness.ActionRef{{ID: harness.ActionStop, Label: "Stop", Role: harness.RoleDestructive}}}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}
}

func (e *Events) setModel(provider, model string) {
	e.state["model"] = provider + "/" + model
	e.state["provider"], e.state["model_id"] = provider, model
	e.state["model_ref"] = modelRef(provider, model)
}

type messagePayload struct {
	Info struct {
		ID         string `json:"id"`
		Role       string `json:"role"`
		SessionID  string `json:"sessionID"`
		ParentID   string `json:"parentID"`
		ModelID    string `json:"modelID"`
		ProviderID string `json:"providerID"`
		Agent      string `json:"agent"`
		Mode       string `json:"mode"`
		Time       struct {
			Created   int64 `json:"created"`
			Completed int64 `json:"completed"`
		} `json:"time"`
		Error *struct {
			Name string `json:"name"`
			Data struct {
				Message    string `json:"message"`
				StatusCode int    `json:"statusCode"`
			} `json:"data"`
		} `json:"error"`
	} `json:"info"`
}

func (e *Events) messageInfo(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p messagePayload
	if json.Unmarshal(raw, &p) != nil || p.Info.ID == "" {
		return nil, nil
	}
	in := p.Info
	if in.Role == "user" && in.SessionID != e.main && e.parentOf(in.SessionID) == "" {
		// A prompt in another top-level session: the TUI switched to it.
		e.setMain(in.SessionID)
	}
	if !e.follows(in.SessionID) {
		return nil, nil
	}
	t := in.ID
	if in.Role != "user" {
		t = in.ParentID
	}
	e.messages[in.ID] = message{role: in.Role, session: in.SessionID, turn: t}
	if len(e.messages) > 4096 {
		e.messages = map[string]message{in.ID: e.messages[in.ID]}
	}
	if in.SessionID == e.main {
		if in.ProviderID != "" && in.ModelID != "" {
			e.setModel(in.ProviderID, in.ModelID)
		}
		if a := in.Agent; a != "" {
			e.state["mode"] = a
		}
	}
	if in.Role == "user" {
		if _, ok := e.turns[in.ID]; ok {
			return nil, nil
		}
		tu := &turn{session: in.SessionID, started: orAt(ms(in.Time.Created), at), state: harness.StateRunning,
			steps: map[string]bool{}, files: map[string]bool{}}
		e.turns[in.ID] = tu
		if in.SessionID == e.main {
			e.state["turn"] = in.ID
			e.state["working"], e.state["idle"] = "true", "false"
		}
		return []harness.Item{e.turnItem(in.ID, tu, nil)}, nil
	}
	if in.Error == nil {
		return nil, nil
	}
	tu := e.turns[t]
	if in.Error.Name == "MessageAbortedError" {
		if tu != nil {
			tu.state = harness.StateInterrupted
		}
		return []harness.Item{e.notice("aborted-"+in.ID, t, e.parentOf(in.SessionID), at, "info", harness.NoticeInterrupted, "Interrupted")}, nil
	}
	if tu != nil {
		tu.state = harness.StateFailed
	}
	return []harness.Item{e.noticeBody("error-"+t, t, e.parentOf(in.SessionID), at,
		errorBody(in.Error.Name, in.Error.Data.StatusCode, in.Error.Data.Message))}, nil
}

// noticeBody is a notice item with a ready body (an error notice).
func (e *Events) noticeBody(id, turn, parent string, at time.Time, b *harness.NoticeBody) harness.Item {
	return harness.Item{ID: id, TurnID: turn, Parent: parent, Kind: harness.KindNotice, At: at, Notice: b}
}

func (e *Events) turnItem(id string, tu *turn, ended *time.Time) harness.Item {
	return harness.Item{ID: id, TurnID: id, Parent: e.parentOf(tu.session), Kind: harness.KindTurn, At: tu.started,
		Turn: &harness.TurnBody{State: tu.state, Started: tu.started, Ended: ended, Steps: len(tu.steps), FilesChanged: len(tu.files)}}
}

func (e *Events) notice(id, turn, parent string, at time.Time, level, code, text string) harness.Item {
	return harness.Item{ID: id, TurnID: turn, Parent: parent, Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: truncate(text, 400)}}
}

func (e *Events) sessionStatus(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		SessionID string `json:"sessionID"`
		Status    struct {
			Type    string `json:"type"`
			Attempt int    `json:"attempt"`
			Message string `json:"message"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &p) != nil || !e.follows(p.SessionID) {
		return nil, nil
	}
	switch p.Status.Type {
	case "busy":
		if p.SessionID == e.main {
			e.state["working"], e.state["idle"] = "true", "false"
		}
	case "idle":
		return e.idle(p.SessionID, at)
	case "retry":
		if p.Status.Message == "Reconnected" {
			return nil, nil // Kilo: the network came back, the call runs again
		}
		// One notice per turn, updated on each attempt.
		t := e.state["turn"]
		return []harness.Item{e.noticeBody("retry-"+t, t, e.parentOf(p.SessionID), at,
			retryBody("", 0, p.Status.Message, p.Status.Attempt))}, nil
	case "offline":
		// Kilo: the provider call failed to connect; Kilo waits for the
		// network and tries again ({"type":"offline","message":"Connection
		// refused"}, Kilo 7.8.27).
		t := e.state["turn"]
		return []harness.Item{e.noticeBody("offline-"+t, t, e.parentOf(p.SessionID), at,
			retryBody("provider.transport", 0, p.Status.Message, 0))}, nil
	}
	return nil, nil
}

// idle ends the session's running turns. A subagent session going idle
// also ends its agent.
func (e *Events) idle(sess string, at time.Time) ([]harness.Item, []harness.Op) {
	if !e.follows(sess) {
		return nil, nil
	}
	if sess == e.main {
		e.state["working"], e.state["idle"] = "false", "true"
	}
	ids := make([]string, 0, len(e.turns))
	for id, tu := range e.turns {
		if tu.session == sess && tu.state != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var items []harness.Item
	for _, id := range ids {
		tu := e.turns[id]
		if tu.state == harness.StateRunning {
			tu.state = harness.StateOK
		}
		items = append(items, e.turnItem(id, tu, ptr(at)))
		tu.state = "" // closed
	}
	s := e.sessions[sess]
	if s == nil || s.agent.Agent == nil || s.agent.Agent.State != harness.StateRunning {
		return items, nil
	}
	x := s.agent
	body := *x.Agent
	body.State, body.Ended = harness.StateOK, ptr(at)
	x.Agent = &body
	s.agent = x
	return append(items, x), []harness.Op{agentRow(x)}
}

// lostTrack forgets whether the main session's turn runs: the stream that
// came back may have missed its end (session.idle, an execution's end),
// and a "working" kept from before would hold the done back for good. The
// pane decides until the next event says otherwise.
func (e *Events) lostTrack() {
	if e.state["working"] == "true" {
		e.state["working"], e.state["idle"] = "false", "true"
	}
}
