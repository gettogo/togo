package hookstyle

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Options tune the Mapper for one harness.
type Options struct {
	// Name is the mapper name the adapter file uses.
	Name string
	// Transcript, when set, finds the session's transcript before any hook
	// names it, so the transcript feed starts without hooks installed
	// (guess.go). It is asked again while no hook has named one, with the
	// one it found last, so it can follow the agent to another
	// conversation; alone says whether no other agent of the kind runs in
	// the folder (source.Solo). It runs away from the session's lock.
	Transcript func(ctx context.Context, env harness.Env, current string, alone func() bool) string
	// Siblings name other programs that write the agent's store.
	Siblings []string
	// Mode maps the payload's permission_mode to the adapter's mode ids;
	// nil keeps the value.
	Mode func(string) string
	// Screen, when set, reads state (the mode) from the pane's text.
	Screen func(text string) map[string]string
	// Replies, when set, reads what the agent said that its hooks do not
	// carry (Copilot's hooks hold no reply text): the messages written to
	// the harness's own log for session after offset, and the offset to go
	// on from. An offset below zero asks only for the log's end. A limit
	// of zero or more reads nothing past it and calls it the log's end.
	Replies func(env harness.Env, session string, offset, limit int64) ([]Reply, int64)
	// Ask, when set, reads the harness's own ask tool: the question a
	// PreToolUse payload for it raises, ok false for any other tool. The
	// call becomes a question item instead of a tool row (ask.go).
	Ask func(tool string, input json.RawMessage) (*harness.QuestionBody, bool)
	// Answered reads the ask tool's PostToolUse response: the answers per
	// question id, every id [] for a cancel; nil when it cannot tell.
	Answered func(tool string, input, response json.RawMessage) map[string][]string
	// Plan, when set, reads the plan text an ask tool that approves a plan
	// carries (Droid's ExitSpecMode): it is drawn as an assistant item just
	// before the question.
	Plan func(tool string, input json.RawMessage) (string, bool)
}

// Reply is one agent message from the harness's own log: its text (empty
// for a message that only requests tools) and the tool calls it requests.
type Reply struct {
	Text  string
	Calls []string
}

// The harness writes its log a moment after the hook fires (Copilot, seen
// live): a read waits this long at most for the message the hook implies,
// the one requesting the tool about to run, or the answer at a stop.
var (
	replyWaitTool = 500 * time.Millisecond
	replyWaitStop = 1500 * time.Millisecond
)

const replyPoll = 50 * time.Millisecond

// Mapper reads normalized hook payloads. It always keeps the session's
// state: the harness's own session id, transcript path, permission mode,
// model, and whether a turn runs. It draws the chat from hooks (prompts,
// tool calls, final replies) only while no transcript feed draws it
// (MarkLive), so a harness whose transcript Togo cannot read still gets a
// conversation. Task and todo hooks become plan rows, subagent hooks
// agent rows.
type Mapper struct {
	harness.PlanMiss
	env  harness.Env
	opts Options

	mu    sync.Mutex
	state map[string]string
	// hooked is set once a hook named the transcript: its word wins.
	hooked  bool
	guess   guess
	turn    string
	started time.Time
	steps   map[string]bool
	planned bool                // a task hook made plan rows
	running map[string][]string // tool name -> running call ids, oldest first
	tools   map[string]harness.Item
	asks    map[string]harness.Item // open ask-tool questions by call id
	n       int
	// replyFrom is where Options.Replies goes on reading replySession's log.
	replySession string
	replyFrom    int64
	// seenCalls are the tool calls the replies read so far requested.
	seenCalls map[string]bool
	// agents are the subagents not stopped yet, by hook agent id.
	agents map[string][]agentRow
	// subs are the subagent ids whose own hooks may arrive, true once
	// stopped (subagents.go).
	subs map[string]bool
	// lastError is the text of the last failure drawn, until a turn begins.
	lastError string
	// closed are the ops end made that the Map call in progress returns.
	closed []harness.Op
	// upTo bounds a replayed record's reads (mark.go).
	upTo *mark
}

// New returns a mapper for one session.
func New(env harness.Env, opts Options) *Mapper {
	m := &Mapper{env: env, opts: opts, state: map[string]string{"working": "false", "idle": "true"},
		steps: map[string]bool{}, running: map[string][]string{}, tools: map[string]harness.Item{},
		asks: map[string]harness.Item{}, seenCalls: map[string]bool{}, agents: map[string][]agentRow{},
		subs: map[string]bool{}}
	m.guess.solo.Also = opts.Siblings
	if env.Cwd != "" {
		// Sources filter the harness's session lists by $state.cwd.
		m.state["cwd"] = env.Cwd
	}
	return m
}

// Register adds a hookstyle mapper under opts.Name.
func Register(opts Options) {
	harness.RegisterMapper(opts.Name, func(env harness.Env) harness.Mapper { return New(env, opts) })
}

func (m *Mapper) Name() string { return m.opts.Name }

// State reports what the hooks said. Until a hook names a transcript,
// Options.Transcript stands in (guess.go).
func (m *Mapper) State() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guessLocked()
	out := make(map[string]string, len(m.state))
	for k, v := range m.state {
		out[k] = v
	}
	return out
}

// ReadScreen reads state from the pane's visible text.
func (m *Mapper) ReadScreen(text string) map[string]string {
	if m.opts.Screen == nil {
		return nil
	}
	return m.opts.Screen(text)
}

type payload struct {
	Event        string          `json:"hook_event_name"`
	SessionID    string          `json:"session_id"`
	Transcript   string          `json:"transcript_path"`
	Mode         string          `json:"permission_mode"`
	Model        json.RawMessage `json:"model"`
	Source       string          `json:"source"`
	Prompt       string          `json:"prompt"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolUseID    string          `json:"tool_use_id"`
	ToolResponse json.RawMessage `json:"tool_response"`
	Error        json.RawMessage `json:"error"`
	ErrorDetails string          `json:"error_details"`
	// ErrorReason is a harness.Err* reason a dialect's prep read from the
	// agent's own error code (failures.go).
	ErrorReason string `json:"error_reason"`
	LastMessage string `json:"last_assistant_message"`
	AgentID     string `json:"agent_id"`
	AgentType   string `json:"agent_type"`
	// ParentAgentID names the agent that started a subagent, on the
	// subagent's own hooks (Copilot's parentAgentId).
	ParentAgentID string `json:"parent_agent_id"`
	// Response is what a subagent answered, on Copilot's subagentStop.
	Response    string `json:"response"`
	TaskID      string `json:"task_id"`
	TaskSubject string `json:"task_subject"`
	TaskDesc    string `json:"task_description"`
	Interrupted bool   `json:"stop_interrupted"`
	// Once marks a tool hook that is the whole call (Amp's tool.result):
	// it starts the call when no earlier hook did.
	Once bool `json:"tool_once"`
}

// Map reads one payload.
func (m *Mapper) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	items, ops := m.mapRecord(rec)
	m.mu.Lock()
	defer m.mu.Unlock()
	ops, m.closed = append(ops, m.closed...), nil
	m.settle()
	return items, ops
}

func (m *Mapper) mapRecord(rec harness.Record) ([]harness.Item, []harness.Op) {
	var p payload
	if json.Unmarshal(rec.Data, &p) != nil {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	draw := !IsLive(m.env.SessionID)
	if m.isSub(p) {
		return m.subHook(p, at, draw), nil
	}
	if !m.fromSub(p) {
		m.learn(p)
	}
	switch p.Event {
	case SessionStart:
		m.idle(at)
		// Another conversation: the earlier one's subagent ids are moot.
		m.subs = map[string]bool{}
		if p.Source == "resume" {
			return m.notice(at, harness.NoticeResumed, "info", "Resumed an earlier conversation"), nil
		}
	case UserPromptSubmit:
		// A prompt while a turn runs (a steer or a queued message): the
		// running turn and its tools end here, else they spin for good.
		var closed []harness.Item
		if m.turn != "" {
			closed = m.end(payload{}, at, draw, harness.StateInterrupted)
		}
		m.begin(at)
		m.skipReplies()
		if !draw {
			return nil, nil
		}
		noteDrawn(m.env.SessionID, m.turn, m.started)
		u := harness.Item{ID: m.turn + ":user", TurnID: m.turn, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: Clip(p.Prompt), Source: "unknown"}}
		return append(closed, u, m.turnItem(harness.StateRunning, nil)), nil
	case PreToolUse:
		if m.turn == "" {
			m.begin(at)
		}
		m.working(at)
		call := p.ToolUseID
		// Waiting pays only for a call whose message has not been read: a
		// hook without an id, or a later call of a message already read,
		// can never match.
		wait := replyWaitTool
		if call == "" || m.seenCalls[call] {
			wait = 0
		}
		said := m.replies(at, draw, wait, func(r Reply) bool { return call != "" && slices.Contains(r.Calls, call) })
		if qs := m.askStart(p, at); len(qs) > 0 {
			if !draw {
				return nil, nil
			}
			return append(append(said, qs...), m.turnItem(harness.StateRunning, nil)), nil
		}
		x := m.toolStart(p, at)
		if !draw {
			return nil, nil
		}
		return append(said, x, m.turnItem(harness.StateRunning, nil)), nil
	case PostToolUse, PostToolUseFailure:
		if q, ok := m.askEnd(p, p.Event == PostToolUseFailure); ok {
			if !draw {
				return nil, nil
			}
			return []harness.Item{q}, nil
		}
		if _, started := m.tools[m.callID(p, false)]; p.Once && !started {
			if m.turn == "" {
				m.begin(at)
			}
			m.toolStart(p, at)
		}
		x, ok := m.toolEnd(p, at, p.Event == PostToolUseFailure)
		if !ok || !draw {
			return nil, nil
		}
		return []harness.Item{x}, nil
	case PermissionRequest:
		m.working(at)
		id := m.callID(p, false)
		x, ok := m.tools[id]
		if !ok || !draw {
			return nil, nil
		}
		b := *x.Tool
		b.State = harness.StateWaiting
		x.Tool = &b
		m.tools[id] = x
		return []harness.Item{x}, nil
	case AgentResponse:
		if !draw || m.turn == "" || p.LastMessage == "" {
			return nil, nil
		}
		m.n++
		return []harness.Item{{ID: m.turn + ":reply-" + strconv.Itoa(m.n), TurnID: m.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: Clip(p.LastMessage)}}}, nil
	case Stop, SessionEnd:
		said := m.replies(at, draw, replyWaitStop, func(r Reply) bool { return len(r.Calls) == 0 })
		if p.Interrupted {
			return append(said, m.end(p, at, draw, harness.StateInterrupted)...), nil
		}
		return append(said, m.end(p, at, draw, harness.StateOK)...), nil
	case StopFailure:
		if raw(p.Error) == "rate_limit" {
			items := m.end(p, at, draw, harness.StateFailed)
			return append(items, m.notice(at, harness.NoticeRateLimit, "error", "Usage limit reached")...), nil
		}
		tid := m.turn
		items := m.end(p, at, draw, harness.StateFailed)
		// The same failure reported again once its turn has ended (Copilot's
		// errorOccurred, then sessionEnd with the error): one notice.
		if tid == "" && p.ErrorDetails != "" && p.ErrorDetails == m.lastError {
			return items, nil
		}
		m.lastError = p.ErrorDetails
		if !draw && p.ErrorDetails == "" && p.ErrorReason == "" {
			// The transcript draws the error itself, in the harness's
			// own words.
			return items, nil
		}
		return append(items, m.errorNotice(at, tid, p, "The turn stopped on an error. The terminal shows the agent's own message.")), nil
	case ErrorOccurred:
		n := m.errorNotice(at, m.turn, p, "The harness reported an error")
		n.Notice.Level = "warning" // the session may go on (a fatal one is StopFailure)
		return []harness.Item{n}, nil
	case PostCompact:
		return m.notice(at, harness.NoticeCompacted, "info", "Conversation compacted"), nil
	case SubagentStart, SubagentStop:
		items, ops := m.agentOps(p, at)
		if !draw {
			items = nil
		}
		return items, ops
	case TaskCreated, TaskCompleted:
		ops := planOps(p)
		m.planned = m.planned || len(ops) > 0
		return nil, ops
	}
	return nil, nil
}

// learn keeps what every payload says about the session.
func (m *Mapper) learn(p payload) {
	if p.SessionID != "" {
		m.state["harness_session"] = p.SessionID
	}
	if p.Transcript != "" {
		m.hooked = true
		m.state["transcript_path"] = p.Transcript
	}
	if p.Mode != "" {
		mode := p.Mode
		if m.opts.Mode != nil {
			mode = m.opts.Mode(mode)
		}
		m.state["mode"] = mode
	}
	if v := modelName(p.Model); v != "" {
		m.state["model"] = v
	}
}

// idle and working record whether a turn runs, stamped with when the
// hook said so: a transcript feed's older "working" then loses to a later
// Stop or StopFailure hook (harness.MergeWorking).
func (m *Mapper) idle(at time.Time) {
	m.state["working"], m.state["idle"] = "false", "true"
	m.state[harness.StateWorkingAt] = harness.StampWorking(at)
}

func (m *Mapper) working(at time.Time) {
	m.state["working"], m.state["idle"] = "true", "false"
	m.state[harness.StateWorkingAt] = harness.StampWorking(at)
}

func (m *Mapper) begin(at time.Time) {
	m.lastError = ""
	m.turn, m.started = "turn-"+strconv.FormatInt(at.UnixMilli(), 10), at
	m.steps = map[string]bool{}
	m.running = map[string][]string{}
	m.tools = map[string]harness.Item{}
	m.asks = map[string]harness.Item{}
	m.state["turn"] = m.turn
	m.working(at)
}

// end closes the turn, with the final reply when the payload carries it.
func (m *Mapper) end(p payload, at time.Time, draw bool, state string) []harness.Item {
	m.idle(at)
	rowState := harness.StateOK
	switch state {
	case harness.StateInterrupted:
		rowState = harness.StateCancelled
	case harness.StateFailed:
		rowState = harness.StateFailed
	}
	agents, ops := m.closeAgents(rowState, at)
	m.closed = append(m.closed, ops...)
	clearDrawn(m.env.SessionID, m.turn)
	if !draw || m.turn == "" {
		return nil
	}
	items := agents
	if p.LastMessage != "" && state == harness.StateOK {
		items = append(items, harness.Item{ID: m.turn + ":reply", TurnID: m.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: Clip(p.LastMessage)}})
	}
	for id, x := range m.tools {
		b := *x.Tool
		b.State, b.Ended = harness.StateInterrupted, &at
		x.Tool = &b
		items = append(items, x)
		delete(m.tools, id)
	}
	items = append(items, m.closeAsks()...)
	items = append(items, m.turnItem(state, &at))
	m.turn = ""
	return items
}

func (m *Mapper) turnItem(state string, ended *time.Time) harness.Item {
	return harness.Item{ID: m.turn, TurnID: m.turn, Kind: harness.KindTurn, At: m.started,
		Turn: &harness.TurnBody{State: state, Started: m.started, Ended: ended, Steps: len(m.steps)}}
}

// callID finds the call a payload is about: its own id, else the oldest
// running call of the same tool (start: a fresh id).
func (m *Mapper) callID(p payload, start bool) string {
	if p.ToolUseID != "" {
		return p.ToolUseID
	}
	if start {
		m.n++
		return m.turn + ":tool-" + strconv.Itoa(m.n)
	}
	if ids := m.running[p.ToolName]; len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (m *Mapper) toolStart(p payload, at time.Time) harness.Item {
	id := m.callID(p, true)
	m.running[p.ToolName] = append(m.running[p.ToolName], id)
	m.steps[id] = true
	title, cat, subj := ToolTitle(p.ToolName, p.ToolInput)
	if !m.planned && isPlanTool(p.ToolName) {
		// Todo calls are read from the task hooks only; with none seen the
		// call stays a plain step and the log says the format is unread.
		m.NoteUnrecognisedPlan()
	}
	x := harness.Item{ID: id, TurnID: m.turn, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: id, Name: p.ToolName, Title: Truncate(title, harness.MaxTitle), Category: cat, Subject: subj,
		State: harness.StateRunning, Started: at}}
	m.tools[id] = x
	return x
}

func (m *Mapper) toolEnd(p payload, at time.Time, failed bool) (harness.Item, bool) {
	id := m.callID(p, false)
	x, ok := m.tools[id]
	if !ok {
		return harness.Item{}, false
	}
	ids := m.running[p.ToolName]
	for i, v := range ids {
		if v == id {
			m.running[p.ToolName] = append(ids[:i:i], ids[i+1:]...)
			break
		}
	}
	delete(m.tools, id)
	b := *x.Tool
	out, bad := ToolOutput(p.ToolResponse)
	b.State, b.Ended, b.Output = harness.StateOK, &at, out
	if failed || bad {
		b.State = harness.StateFailed
	}
	x.Tool = &b
	return x, true
}
