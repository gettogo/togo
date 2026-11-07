package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

// partialEvery bounds streamed updates of one item.
const partialEvery = 250 * time.Millisecond

// AppServer maps the Codex app-server feed. It follows several threads:
// the main thread (the TUI's) and subagent threads, whose items go into the
// subagent's own conversation.
type AppServer struct {
	env harness.Env
	cwd string // the main thread's folder, for relative file paths

	mu      sync.Mutex
	state   map[string]string
	main    string
	threads map[string]*threadInfo
	turns   map[string]*turnInfo
	streams map[string]*stream
	tools   map[string]harness.Item
	started map[string]time.Time
	status  harness.StatusBlock
	planned map[string]time.Time // plan item id -> first time, kept on updates
}

type threadInfo struct {
	parent string // the agent item id of a subagent thread; "" for top level
	top    bool
}

type turnInfo struct {
	thread  string
	started time.Time
	steps   map[string]bool
	files   map[string]bool
}

type stream struct {
	kind  string
	turn  string
	text  string
	index int64
	last  time.Time
}

// NewAppServer returns a mapper for one session.
func NewAppServer(env harness.Env) *AppServer {
	return &AppServer{
		env:     env,
		state:   map[string]string{"working": "false", "idle": "true"},
		threads: map[string]*threadInfo{},
		turns:   map[string]*turnInfo{},
		streams: map[string]*stream{},
		tools:   map[string]harness.Item{},
		started: map[string]time.Time{},
		planned: map[string]time.Time{},
	}
}

func (a *AppServer) Name() string { return MapperAppServer }

// State reports model, effort, mode (the approval policy),
// harness_session (the main thread id), turn, working and idle.
func (a *AppServer) State() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]string, len(a.state))
	for k, v := range a.state {
		out[k] = v
	}
	return out
}

// Map turns one feed record into items and mission ops.
func (a *AppServer) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var m msg
	if json.Unmarshal(rec.Data, &m) != nil {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	if m.isRequest() {
		return a.request(m, at)
	}
	switch m.Method {
	case rpc.MethodClosed:
		unmarkLive(a.env.SessionID)
		a.state["working"], a.state["idle"] = "false", "true"
		return a.dropQuestions()
	case "thread/resume":
		return a.resumed(m.Params, at)
	case "turn/started":
		return a.turnStarted(m.Params, at)
	case "turn/completed":
		return a.turnCompleted(m.Params, at)
	case "item/started", "item/completed":
		return a.itemEvent(m.Params, at, m.Method == "item/completed")
	case "item/agentMessage/delta":
		return a.delta(m.Params, at, harness.KindAssistant)
	case "item/reasoning/summaryTextDelta":
		return a.delta(m.Params, at, harness.KindThinking)
	case "item/commandExecution/outputDelta":
		return a.output(m.Params, at)
	case "turn/plan/updated":
		return a.plan(m.Params, at)
	case "turn/diff/updated":
		return a.diff(m.Params)
	case "thread/tokenUsage/updated":
		return nil, a.tokens(m.Params)
	case "account/rateLimits/updated":
		return nil, a.limits(m.Params)
	case "thread/status/changed":
		return a.threadStatus(m.Params)
	case "thread/settings/updated":
		return a.settings(m.Params, at)
	case "thread/compacted":
		return a.compacted(m.Params, at), nil
	case "model/rerouted":
		return a.rerouted(m.Params, at), nil
	case "error":
		return a.errorNotice(m.Params, at), nil
	case "serverRequest/resolved":
		return a.resolved(m.Params, at)
	}
	return nil, nil
}

func (a *AppServer) parentOf(thread string) string {
	if t := a.threads[thread]; t != nil {
		return t.parent
	}
	return ""
}

func (a *AppServer) setMain(thread string) {
	a.main = thread
	a.state["harness_session"] = thread
}

type resumeResult struct {
	Thread struct {
		ID             string  `json:"id"`
		ParentThreadID *string `json:"parentThreadId"`
		AgentNickname  *string `json:"agentNickname"`
		AgentRole      *string `json:"agentRole"`
		Name           *string `json:"name"`
		Status         struct {
			Type string `json:"type"`
		} `json:"status"`
		Turns []turnPayload `json:"turns"`
	} `json:"thread"`
	Model           string          `json:"model"`
	Cwd             string          `json:"cwd"`
	ReasoningEffort *string         `json:"reasoningEffort"`
	ApprovalPolicy  json.RawMessage `json:"approvalPolicy"`
}

type turnPayload struct {
	ID          string       `json:"id"`
	Items       []threadItem `json:"items"`
	Status      string       `json:"status"`
	StartedAt   int64        `json:"startedAt"`
	CompletedAt int64        `json:"completedAt"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// resumed handles the result of joining a thread: thread facts, settings
// and the thread's history so far.
func (a *AppServer) resumed(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		Result resumeResult `json:"result"`
	}
	if json.Unmarshal(params, &p) != nil || p.Result.Thread.ID == "" {
		return nil, nil
	}
	markLive(a.env.SessionID, at)
	r := p.Result
	id := r.Thread.ID
	var items []harness.Item
	var ops []harness.Op
	if r.Thread.ParentThreadID == nil || *r.Thread.ParentThreadID == "" {
		a.threads[id] = &threadInfo{top: true}
		a.setMain(id)
		if r.Cwd != "" {
			a.cwd = r.Cwd
		}
		a.applySettings(r.Model, r.ReasoningEffort, r.ApprovalPolicy)
		ops = append(ops, a.statusOp())
	} else {
		agentID := "agent-" + id
		a.threads[id] = &threadInfo{parent: agentID}
		name := "Subagent"
		if r.Thread.AgentNickname != nil && *r.Thread.AgentNickname != "" {
			name = *r.Thread.AgentNickname
		} else if r.Thread.Name != nil && *r.Thread.Name != "" {
			name = *r.Thread.Name
		}
		role := ""
		if r.Thread.AgentRole != nil {
			role = *r.Thread.AgentRole
		}
		st := harness.StateIdle
		if r.Thread.Status.Type == "active" {
			st = harness.StateRunning
		}
		x := harness.Item{ID: agentID, Parent: a.parentOf(*r.Thread.ParentThreadID), Kind: harness.KindAgent, At: at,
			Agent: &harness.AgentBody{AgentID: id, Name: truncate(name, harness.MaxTitle), Role: role, State: st, Started: at}}
		items = append(items, x)
		ops = append(ops, agentRow(x))
	}
	for _, t := range r.Thread.Turns {
		items = append(items, a.history(id, t, at)...)
	}
	return items, ops
}

// history replays one finished or running turn from a resume result.
func (a *AppServer) history(thread string, t turnPayload, at time.Time) []harness.Item {
	started := orNow(secs(t.StartedAt), at)
	var ended *time.Time
	if e := secs(t.CompletedAt); !e.IsZero() {
		ended = &e
	}
	w := where{turn: t.ID, parent: a.parentOf(thread), started: started, ended: ended, at: started}
	ti := a.turn(t.ID, thread, started)
	var items []harness.Item
	for _, it := range t.Items {
		for _, x := range convert(it, w, true) {
			a.count(ti, &x)
			items = append(items, x)
		}
	}
	items = append(items, a.turnItem(t.ID, ti, t.Status, ended, at))
	if t.Status == "inProgress" && thread == a.main {
		a.state["turn"] = t.ID
		a.state["working"], a.state["idle"] = "true", "false"
	}
	return items
}

func (a *AppServer) turn(id, thread string, started time.Time) *turnInfo {
	ti := a.turns[id]
	if ti == nil {
		ti = &turnInfo{thread: thread, started: started, steps: map[string]bool{}, files: map[string]bool{}}
		a.turns[id] = ti
		if len(a.turns) > 512 {
			a.trimTurns()
		}
	}
	return ti
}

// trimTurns forgets the oldest finished turns' bookkeeping.
func (a *AppServer) trimTurns() {
	cut := time.Now().Add(-24 * time.Hour)
	for id, t := range a.turns {
		if t.started.Before(cut) {
			delete(a.turns, id)
		}
	}
}

// count makes a tool item's file paths relative to the session folder and
// adds the item to its turn's steps and files.
func (a *AppServer) count(ti *turnInfo, x *harness.Item) {
	if x.Tool == nil {
		return
	}
	for i := range x.Tool.Files {
		x.Tool.Files[i].Path = a.rel(x.Tool.Files[i].Path)
	}
	if ti == nil {
		return
	}
	ti.steps[x.ID] = true
	for _, f := range x.Tool.Files {
		ti.files[f.Path] = true
	}
}

// rel turns an absolute path under the session folder into a relative one.
func (a *AppServer) rel(p string) string {
	for _, root := range []string{a.env.Cwd, a.cwd} {
		if root == "" || !filepath.IsAbs(p) {
			continue
		}
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func (a *AppServer) turnItem(id string, ti *turnInfo, status string, ended *time.Time, at time.Time) harness.Item {
	st := runState(status)
	if status == "interrupted" {
		st = harness.StateInterrupted
	}
	body := &harness.TurnBody{State: st, Started: ti.started, Steps: len(ti.steps), FilesChanged: len(ti.files)}
	if st != harness.StateRunning {
		body.Ended = ended
		if body.Ended == nil {
			body.Ended = ptr(at)
		}
	}
	return harness.Item{ID: id, TurnID: id, Parent: a.parentOf(ti.thread), Kind: harness.KindTurn, At: ti.started, Turn: body}
}

func (a *AppServer) turnStarted(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string      `json:"threadId"`
		Turn     turnPayload `json:"turn"`
	}
	if json.Unmarshal(params, &p) != nil || p.Turn.ID == "" {
		return nil, nil
	}
	if t := a.threads[p.ThreadID]; (t == nil || t.top) && p.ThreadID != a.main {
		// A new top-level thread started a turn: the desk ran /new or
		// /resume, so it is now the main thread.
		a.threads[p.ThreadID] = &threadInfo{top: true}
		a.setMain(p.ThreadID)
	}
	started := orNow(secs(p.Turn.StartedAt), at)
	ti := a.turn(p.Turn.ID, p.ThreadID, started)
	if p.ThreadID == a.main {
		a.state["turn"] = p.Turn.ID
		a.state["working"], a.state["idle"] = "true", "false"
	}
	return []harness.Item{a.turnItem(p.Turn.ID, ti, "inProgress", nil, at)}, nil
}

func (a *AppServer) turnCompleted(params json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var p struct {
		ThreadID string      `json:"threadId"`
		Turn     turnPayload `json:"turn"`
	}
	if json.Unmarshal(params, &p) != nil || p.Turn.ID == "" {
		return nil, nil
	}
	ti := a.turn(p.Turn.ID, p.ThreadID, orNow(secs(p.Turn.StartedAt), at))
	var ended *time.Time
	if e := secs(p.Turn.CompletedAt); !e.IsZero() {
		ended = &e
	}
	if p.ThreadID == a.main {
		a.state["working"], a.state["idle"] = "false", "true"
	}
	// A subagent's own state comes from its collab call, not from the end
	// of one of its turns.
	return []harness.Item{a.turnItem(p.Turn.ID, ti, p.Turn.Status, ended, at)}, nil
}

type itemParams struct {
	Item        threadItem `json:"item"`
	ThreadID    string     `json:"threadId"`
	TurnID      string     `json:"turnId"`
	StartedAtMs int64      `json:"startedAtMs"`
	DoneAtMs    int64      `json:"completedAtMs"`
}

func (a *AppServer) itemEvent(params json.RawMessage, at time.Time, final bool) ([]harness.Item, []harness.Op) {
	var p itemParams
	if json.Unmarshal(params, &p) != nil || p.Item.ID == "" {
		return nil, nil
	}
	id := p.Item.ID
	started := a.started[id]
	if !final {
		started = orNow(ms(p.StartedAtMs), at)
		a.started[id] = started
	}
	if started.IsZero() {
		started = at
	}
	w := where{turn: p.TurnID, parent: a.parentOf(p.ThreadID), started: started, at: started}
	if final {
		e := orNow(ms(p.DoneAtMs), at)
		w.ended = &e
		delete(a.started, id)
		delete(a.streams, id)
	}
	ti := a.turns[p.TurnID]
	items := convert(p.Item, w, final)
	for i := range items {
		a.count(ti, &items[i])
		x := items[i]
		if x.Tool != nil {
			if final {
				delete(a.tools, x.ID)
			} else {
				a.tools[x.ID] = x
			}
		}
	}
	var ops []harness.Op
	if p.Item.Type == "collabAgentToolCall" {
		ag, agOps := a.collab(p.Item, p.ThreadID, at)
		items = append(items, ag...)
		ops = append(ops, agOps...)
	}
	return items, ops
}

// collab announces the agents a collab call spawned or reports on.
func (a *AppServer) collab(it threadItem, thread string, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	name := "Subagent"
	if it.Prompt != nil && *it.Prompt != "" {
		name = strings.TrimSpace(strings.SplitN(*it.Prompt, "\n", 2)[0])
	}
	for _, tid := range it.ReceiverThreadIDs {
		agentID := "agent-" + tid
		if a.threads[tid] == nil {
			a.threads[tid] = &threadInfo{parent: agentID}
		}
		st := harness.StateRunning
		if s, ok := it.AgentsStates[tid]; ok {
			st = runState(s.Status)
		}
		body := &harness.AgentBody{AgentID: tid, Name: truncate(name, harness.MaxTitle), State: st, Started: at}
		if st != harness.StateRunning {
			body.Ended = ptr(at)
		}
		x := harness.Item{ID: agentID, Parent: a.parentOf(thread), Kind: harness.KindAgent, At: at, Agent: body}
		items = append(items, x)
		ops = append(ops, agentRow(x))
	}
	return items, ops
}

func agentRow(x harness.Item) harness.Op {
	row := harness.Row{ID: x.Agent.AgentID, Title: x.Agent.Name, Subtitle: x.Agent.Role, State: x.Agent.State,
		Started: ptr(x.Agent.Started), Open: &harness.Open{Kind: harness.OpenChat, Ref: x.ID}}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}
}
