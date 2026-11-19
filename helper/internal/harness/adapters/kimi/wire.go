package kimi

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// Wire maps Kimi's wire log. Records carry no session id, so the mapper
// treats the stream as one conversation; a metadata header starts a new
// file and ends whatever turn was open.
type Wire struct {
	harness.PlanMiss
	env harness.Env

	mu       sync.Mutex
	state    map[string]string
	file     string // the log the records come from
	permMode string
	planMode bool
	turn     *turn
	texts    map[string]*harness.Item // assistant and thinking text by step
	tools    map[string]harness.Item  // by tool call id
	tasks    map[string]harness.Item  // agents and background work by task id
	byCall   map[string]string        // tool call id -> task id
	open     map[string]harness.Item  // unanswered questions and approvals by interaction id
	status   harness.StatusBlock
	// endsTurns: the log says when a turn ends (protocol 1.5 writes
	// turn.ended), so a step's end_turn finish does not end it.
	endsTurns bool
	// sections are the mission sections the log has put rows in.
	sections map[string]bool
}

type turn struct {
	id      string
	started time.Time
	steps   int
	files   map[string]bool
}

// NewWire returns a mapper for one session.
func NewWire(env harness.Env) *Wire {
	return &Wire{env: env, state: map[string]string{"working": "false", "mode": initialMode(env.Argv, defaultPlanMode())}, texts: map[string]*harness.Item{},
		tools: map[string]harness.Item{}, tasks: map[string]harness.Item{}, byCall: map[string]string{},
		open: map[string]harness.Item{}, sections: map[string]bool{}}
}

func (w *Wire) Name() string { return MapperWire }

// State reports model, effort, mode (default, yolo, auto or plan), turn and
// working.
func (w *Wire) State() map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]string, len(w.state))
	for k, v := range w.state {
		out[k] = v
	}
	return out
}

type record struct {
	Type          string          `json:"type"`
	Protocol      string          `json:"protocol_version"`
	Time          int64           `json:"time"`
	ModelAlias    string          `json:"modelAlias"`
	Effort        string          `json:"thinkingEffort"`
	Mode          string          `json:"mode"`
	TurnID        json.RawMessage `json:"turnId"`
	PromptID      string          `json:"promptId"`
	Reason        string          `json:"reason"`
	Input         []part          `json:"input"`
	Origin        origin          `json:"origin"`
	Event         json.RawMessage `json:"event"`
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	ToolCallID    string          `json:"toolCallId"`
	Request       json.RawMessage `json:"request"`
	Response      json.RawMessage `json:"response"`
	Info          json.RawMessage `json:"info"`
	CompactionSum string          `json:"summary"`
	Error         *wireError      `json:"error"`
}

// wireError is the error a failed turn.ended carries (protocol 1.5).
type wireError struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Message string `json:"message"`
	Details struct {
		StatusCode int `json:"statusCode"`
		MaxSteps   int `json:"maxSteps"`
	} `json:"details"`
}

type part struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Think string `json:"think"`
}

type origin struct {
	Kind   string `json:"kind"`
	TaskID string `json:"taskId"`
	Status string `json:"status"`
}

func (w *Wire) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var r record
	if json.Unmarshal(rec.Data, &r) != nil || r.Type == "" {
		return nil, nil
	}
	at := x.Or(x.Millis(r.Time), rec.At).UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if rec.File != "" {
		if w.file != "" && rec.File != w.file {
			harness.Moved(w.state) // a /new or a resume: another log
		}
		w.file = rec.File
	}
	items, ops := w.mapRecord(r, at)
	for _, op := range ops {
		if op.Row != nil || len(op.Rows) > 0 {
			w.sections[op.Section] = true
		}
	}
	return items, ops
}

func (w *Wire) mapRecord(r record, at time.Time) ([]harness.Item, []harness.Op) {
	switch r.Type {
	case "metadata":
		// A new log (/new, a restart): nothing of the old one goes on.
		w.endsTurns = protocolAtLeast(r.Protocol, 1, 5)
		items, ops := w.endTurn(harness.StateInterrupted, at)
		return items, append(ops, w.clearSections()...)
	case "profile.bind", "config.update", "llm.request":
		return nil, w.setModel(r.ModelAlias, r.Effort)
	case "permission.set_mode":
		w.permMode = r.Mode
		return w.modeChanged(at)
	case "plan_mode.enter", "plan_mode.exit":
		w.planMode = r.Type == "plan_mode.enter"
		return w.modeChanged(at)
	case "turn.prompt":
		return w.prompt(r, at)
	case "agent.turn.started":
		w.state["working"] = "true"
	case "turn.steer":
		return w.steer(r, at)
	case "context.append_loop_event":
		return w.loopEvent(r.Event, at)
	case "interaction.request":
		return w.interaction(r, at), nil
	case "interaction.resolved":
		return w.resolved(r), nil
	case "turn.cancel":
		return w.endTurn(harness.StateInterrupted, at)
	case "turn.ended":
		w.endsTurns = true
		switch r.Reason {
		case "cancelled":
			return w.endTurn(harness.StateInterrupted, at)
		case "failed", "blocked":
			return w.failTurn(r, at)
		}
		return w.endTurn(harness.StateOK, at)
	case "full_compaction.complete":
		return []harness.Item{{ID: "compact-" + strconv.FormatInt(r.Time, 10), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: "Conversation compacted"}}}, nil
	case "task.started":
		return w.taskStarted(r.Info, at)
	}
	return nil, nil
}

// clearSections empties the plan, agent and background rows the log made,
// and forgets the tasks they stood for.
func (w *Wire) clearSections() []harness.Op {
	w.tasks, w.byCall = map[string]harness.Item{}, map[string]string{}
	var ops []harness.Op
	for _, s := range []string{harness.SectionPlan, harness.SectionAgents, harness.SectionBackground} {
		if w.sections[s] {
			ops = append(ops, harness.Op{Op: harness.OpReplace, Section: s, Rows: []harness.Row{}})
		}
	}
	w.sections = map[string]bool{}
	return ops
}

// protocolAtLeast reads a "major.minor" protocol version.
func protocolAtLeast(v string, major, minor int) bool {
	a, b, ok := strings.Cut(v, ".")
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil {
		return false
	}
	return x > major || (x == major && y >= minor)
}

func (w *Wire) setModel(model, effort string) []harness.Op {
	changed := false
	if model != "" && w.state["model"] != model {
		w.state["model"], changed = model, true
	}
	if effort != "" && w.state["effort"] != effort {
		w.state["effort"], changed = effort, true
	}
	if !changed {
		return nil
	}
	return []harness.Op{w.statusOp()}
}

func (w *Wire) modeChanged(at time.Time) ([]harness.Item, []harness.Op) {
	mode := w.permMode
	if mode == "" || mode == "manual" {
		// Kimi Code 2.5 names the default (ask) mode "manual" (seen live).
		mode = "default"
	}
	if w.planMode {
		mode = "plan"
	}
	if w.state["mode"] == mode {
		return nil, nil
	}
	w.state["mode"] = mode
	return nil, []harness.Op{w.statusOp()}
}

func (w *Wire) statusOp() harness.Op {
	st := harness.StatusBlock{Model: w.state["model"], Mode: w.state["mode"], Effort: w.state["effort"]}
	return harness.Op{Op: harness.OpStatus, Status: &st}
}

// turnKey reads a turn id written as a number or a string.
func turnKey(raw json.RawMessage) string {
	return strings.Trim(string(raw), `"`)
}

func joinText(ps []part) string {
	var b []string
	for _, p := range ps {
		if p.Type == "text" && p.Text != "" {
			b = append(b, p.Text)
		}
	}
	return strings.Join(b, "\n")
}

func (w *Wire) prompt(r record, at time.Time) ([]harness.Item, []harness.Op) {
	items, ops := w.endTurn(harness.StateInterrupted, at)
	key := turnKey(r.TurnID)
	if key == "" {
		key = strconv.FormatInt(at.UnixMilli(), 10)
	}
	t := &turn{id: "turn-" + key + "-" + strconv.FormatInt(at.UnixMilli(), 36), started: at, files: map[string]bool{}}
	w.turn = t
	w.state["turn"], w.state["working"] = t.id, "true"
	// A skill the user runs (a slash command) starts its turn the same way.
	if r.Origin.Kind == "user" || r.Origin.Kind == "skill_activation" {
		id := r.PromptID
		if id == "" {
			id = "u-" + t.id
		}
		items = append(items, harness.Item{ID: id, TurnID: t.id, Kind: harness.KindUser, At: at,
			User: &harness.UserBody{Text: x.Clip(joinText(r.Input)), Source: "unknown"}})
	}
	items = append(items, w.turnItem(harness.StateRunning, nil))
	if r.Origin.Kind == "background_task" {
		// A task that finished while Kimi sat idle starts the turn that
		// reports it, as a steer would have mid-turn.
		done, doneOps := w.taskDone(r.Origin.TaskID, r.Origin.Status, at)
		items, ops = append(items, done...), append(ops, doneOps...)
	}
	return items, ops
}

func (w *Wire) turnItem(state string, ended *time.Time) harness.Item {
	t := w.turn
	return harness.Item{ID: t.id, TurnID: t.id, Kind: harness.KindTurn, At: t.started,
		Turn: &harness.TurnBody{State: state, Started: t.started, Ended: ended, Steps: t.steps, FilesChanged: len(t.files)}}
}

func (w *Wire) endTurn(state string, at time.Time) ([]harness.Item, []harness.Op) {
	w.state["working"] = "false"
	// A prompt still open when its turn ends is never answered (the turn
	// was cancelled, or Kimi was killed and continued in this log).
	items := w.closeOpen()
	if w.turn == nil {
		return items, nil
	}
	for id, tool := range w.tools {
		if tool.Tool.State == harness.StateRunning {
			tool.Tool.State, tool.Tool.Ended = harness.StateInterrupted, x.Ptr(at)
			items = append(items, tool)
		}
		delete(w.tools, id)
	}
	items = append(items, w.turnItem(state, x.Ptr(at)))
	w.turn, w.texts = nil, map[string]*harness.Item{}
	return items, nil
}

// steer handles prompts typed while the agent works and background task
// notifications.
func (w *Wire) steer(r record, at time.Time) ([]harness.Item, []harness.Op) {
	switch r.Origin.Kind {
	case "user":
		id := "steer-" + strconv.FormatInt(r.Time, 10)
		it := harness.Item{ID: id, Kind: harness.KindUser, At: at, User: &harness.UserBody{Text: x.Clip(joinText(r.Input)), Source: "unknown"}}
		if w.turn != nil {
			it.TurnID = w.turn.id
		}
		return []harness.Item{it}, nil
	case "background_task":
		return w.taskDone(r.Origin.TaskID, r.Origin.Status, at)
	}
	return nil, nil
}
