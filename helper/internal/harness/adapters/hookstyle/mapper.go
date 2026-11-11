package hookstyle

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
)

// Options tune the Mapper for one harness.
type Options struct {
	// Name is the mapper name the adapter file uses.
	Name string
	// Transcript, when set, finds the session's transcript before any hook
	// names it, so the transcript feed starts without hooks installed.
	Transcript func(env harness.Env) string
	// Mode maps the payload's permission_mode to the adapter's mode ids;
	// nil keeps the value.
	Mode func(string) string
	// Screen, when set, reads state (the mode) from the pane's text.
	Screen func(text string) map[string]string
	// Replies, when set, reads what the agent said that its hooks do not
	// carry (Copilot's hooks hold no reply text): the messages written to
	// the harness's own log for session after offset, and the offset to go
	// on from. An offset below zero asks only for the log's end.
	Replies func(env harness.Env, session string, offset int64) ([]Reply, int64)
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
	env  harness.Env
	opts Options

	mu        sync.Mutex
	state     map[string]string
	guessedAt time.Time
	turn      string
	started   time.Time
	steps     map[string]bool
	running   map[string][]string // tool name -> running call ids, oldest first
	tools     map[string]harness.Item
	asks      map[string]harness.Item // open ask-tool questions by call id
	n         int
	// replyFrom is where Options.Replies goes on reading replySession's log.
	replySession string
	replyFrom    int64
}

// New returns a mapper for one session.
func New(env harness.Env, opts Options) *Mapper {
	m := &Mapper{env: env, opts: opts, state: map[string]string{"working": "false", "idle": "true"},
		steps: map[string]bool{}, running: map[string][]string{}, tools: map[string]harness.Item{},
		asks: map[string]harness.Item{}}
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

// State reports what the hooks said. Before any hook names a transcript,
// Options.Transcript stands in, checked at most every 5 seconds.
func (m *Mapper) State() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state["transcript_path"] == "" && m.opts.Transcript != nil && time.Since(m.guessedAt) > 5*time.Second {
		m.guessedAt = time.Now()
		if p := m.opts.Transcript(m.env); p != "" {
			m.state["transcript_path"] = p
		}
	}
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
	m.learn(p)
	draw := !IsLive(m.env.SessionID)
	switch p.Event {
	case SessionStart:
		m.idle(at)
		if p.Source == "resume" {
			return m.notice(at, harness.NoticeResumed, "info", "Resumed an earlier conversation"), nil
		}
	case UserPromptSubmit:
		m.begin(at)
		m.skipReplies()
		if !draw {
			return nil, nil
		}
		noteDrawn(m.env.SessionID, m.turn, m.started)
		u := harness.Item{ID: m.turn + ":user", TurnID: m.turn, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: Clip(p.Prompt), Source: "unknown"}}
		return []harness.Item{u, m.turnItem(harness.StateRunning, nil)}, nil
	case PreToolUse:
		if m.turn == "" {
			m.begin(at)
		}
		m.working(at)
		call := p.ToolUseID
		said := m.replies(at, draw, replyWaitTool, func(r Reply) bool { return call != "" && slices.Contains(r.Calls, call) })
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
		return nil, agentOps(p, at)
	case TaskCreated, TaskCompleted:
		return nil, planOps(p)
	}
	return nil, nil
}

// learn keeps what every payload says about the session.
func (m *Mapper) learn(p payload) {
	if p.SessionID != "" {
		m.state["harness_session"] = p.SessionID
	}
	if p.Transcript != "" {
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
	clearDrawn(m.env.SessionID, m.turn)
	if !draw || m.turn == "" {
		return nil
	}
	var items []harness.Item
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

func (m *Mapper) notice(at time.Time, code, level, text string) []harness.Item {
	m.n++
	return []harness.Item{{
		ID: fmt.Sprintf("hook-notice-%d-%d", at.UnixMilli(), m.n), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: level, Code: code, Text: text},
	}}
}

// errorNotice says why a turn failed: the reason the prep read, else one
// picked from the agent's words, else fallback.
func (m *Mapper) errorNotice(at time.Time, turn string, p payload, fallback string) harness.Item {
	m.n++
	var body *harness.NoticeBody
	switch {
	case p.ErrorReason != "":
		body = errtext.Of(p.ErrorReason, p.ErrorDetails)
	case p.ErrorDetails != "":
		body = errtext.Notice(p.ErrorDetails, 0, fallback)
	default:
		body = harness.ErrorNotice(harness.ErrOther, fallback)
	}
	return harness.Item{ID: fmt.Sprintf("hook-notice-%d-%d", at.UnixMilli(), m.n), TurnID: turn, Kind: harness.KindNotice, At: at, Notice: body}
}

// skipReplies starts reading the harness's log at its end when a turn
// begins in a session not read yet, so an earlier conversation's messages
// stay out of this turn.
func (m *Mapper) skipReplies() {
	session := m.state["harness_session"]
	if m.opts.Replies == nil || session == "" || session == m.replySession {
		return
	}
	_, end := m.opts.Replies(m.env, session, -1)
	m.replySession, m.replyFrom = session, end
}

// replies returns the agent's messages written since the last read, as
// the turn's assistant items. It reads again, for up to wait, until one of
// them is the message want looks for.
func (m *Mapper) replies(at time.Time, draw bool, wait time.Duration, want func(Reply) bool) []harness.Item {
	if m.opts.Replies == nil || m.turn == "" {
		return nil
	}
	session := m.state["harness_session"]
	if session == "" {
		return nil
	}
	if session != m.replySession {
		m.replySession, m.replyFrom = session, 0
	}
	var got []Reply
	deadline := time.Now().Add(wait)
	for {
		more, next := m.opts.Replies(m.env, session, m.replyFrom)
		m.replyFrom = next
		got = append(got, more...)
		if slices.ContainsFunc(got, want) || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(replyPoll)
	}
	if !draw {
		return nil
	}
	var items []harness.Item
	for _, r := range got {
		t := strings.TrimSpace(r.Text)
		if t == "" {
			continue
		}
		m.n++
		items = append(items, harness.Item{ID: m.turn + ":reply-" + strconv.Itoa(m.n), TurnID: m.turn, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: Clip(t)}})
	}
	return items
}
