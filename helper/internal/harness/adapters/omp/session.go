package omp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
)

// session maps omp session JSONL (~/.omp/agent/sessions/<dir>/<ts>_<id>.jsonl
// and its subagents' <session>/<task>.jsonl files) into items. One instance
// serves both feeds of a session. Entries form a tree by id and parentId,
// so an entry belongs to the file whose entries it continues; an entry
// without a known parent belongs to the file whose header came last.
type session struct {
	harness.PlanMiss
	env harness.Env
	cwd string

	mu        sync.Mutex
	state     map[string]string
	dirAt     time.Time
	readAt    time.Time         // when the record being mapped was read
	crumbAt   time.Time         // when the terminal record was last read
	crumb     string            // the session file omp's terminal record names
	parents   map[string]string // entry id -> agent item id ("" for the main session)
	cur       string            // parent of the file read last
	turn      *turn
	tools     map[string]*harness.Item
	asks      map[string]*harness.Item // ask call id -> its open question item
	jobs      map[string]*harness.Item // async jobs by omp job id (jobs.go)
	taskCalls map[string][]string      // task call id -> agent item ids
	pending   []string                 // agent item ids still waiting for their file
	detached  map[string]bool          // agent item ids of background task calls
	agents    map[string]*harness.Item
	headers   map[string]string // subagent header id -> agent item id
	files     map[string]bool
	byFile    agentFiles // which agent each subagent file is (files.go)
	cost      float64
	usage     map[string]any

	todo       []todoPhase            // the main session's plan
	todoBefore map[string][]todoPhase // todo call id -> the plan before it
	todoUnread map[string]bool        // todo calls whose arguments gave no rows
}

// dropped owns the entries of files that are not part of the session.
const dropped = "-"

type turn struct {
	item  harness.Item
	files map[string]bool
	ended bool
}

func newSession(env harness.Env) *session {
	return &session{env: env, cwd: env.Cwd, state: map[string]string{}, parents: map[string]string{},
		tools: map[string]*harness.Item{}, asks: map[string]*harness.Item{}, taskCalls: map[string][]string{}, agents: map[string]*harness.Item{},
		headers: map[string]string{}, files: map[string]bool{}, detached: map[string]bool{},
		todoBefore: map[string][]todoPhase{}, todoUnread: map[string]bool{}, jobs: map[string]*harness.Item{}, byFile: newAgentFiles()}
}

func (s *session) Name() string { return MapperSession }

// State reports model, effort, mode, harness_session, title, turn, working
// and session_dir (the folder the feeds read).
func (s *session) State() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state["session_dir"] == "" || time.Since(s.dirAt) > 5*time.Second && !s.found() {
		s.dirAt = time.Now()
		if d := sessionDir(s.env); d != "" {
			s.state["session_dir"] = d
		}
	}
	if time.Since(s.crumbAt) >= time.Second {
		s.crumbAt = time.Now()
		s.crumb = terminalSession(s.env)
	}
	out := make(map[string]string, len(s.state)+4)
	for k, v := range s.state {
		out[k] = v
	}
	// The feeds' files, under ~/.omp/agent/sessions: the one omp's terminal
	// record names, else the folder's (claimed by process evidence), with
	// subagents only from this session's own folder.
	switch {
	case s.crumb != "":
		out["session_rel"] = s.crumb
		out["subagent_rel"] = strings.TrimSuffix(s.crumb, ".jsonl") + "/*.jsonl"
	case s.state["session_dir"] != "":
		out["session_rel"] = s.state["session_dir"] + "/*.jsonl"
		if hs := s.state["harness_session"]; hs != "" {
			out["subagent_rel"] = s.state["session_dir"] + "/*_" + hs + "/*.jsonl"
		}
	}
	if s.turn != nil {
		out["turn"] = s.turn.item.ID
		out["working"] = strconv.FormatBool(!s.turn.ended)
	}
	return out
}

// PaneMoved takes the session's environment after omp was restarted in its
// pane (a resume): the record to read is the one omp writes for the pane's
// new device, and the old device's, which still names the conversation
// before the restart, is not read again. It is read at once.
func (s *session) PaneMoved(env harness.Env) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.env = env
	s.crumbAt, s.crumb = time.Time{}, ""
}

// found reports whether a session file was read, so the folder is known.
func (s *session) found() bool { return s.state["harness_session"] != "" }

// Map reads one session line. Unknown entry types are skipped.
func (s *session) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	e := decode(rec.Data)
	if e == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, ops := s.mapEntry(rec, e)
	if rec.Source == feedSession {
		s.byFile.passed()
	}
	return s.byFile.screen(items, ops)
}

// mapEntry maps one decoded entry. The caller holds the lock.
func (s *session) mapEntry(rec harness.Record, e map[string]any) ([]harness.Item, []harness.Op) {
	at := stamp(e["timestamp"], rec.At)
	s.readAt = rec.At
	main := rec.Source == feedSession
	if main {
		s.byFile.mainRead(e, at)
	} else if a, ok := s.byFile.agent[rec.File]; ok && rec.File != "" {
		// An entry of a file whose header was read: that file's agent,
		// whichever file's header came last.
		s.cur = a
	}
	if str(e, "type") == "session" {
		return s.header(e, at, main, rec.File)
	}
	// Every entry is placed, also the ones not shown: a note omp adds (a
	// background result, a reminder) is the parent of what the agent says
	// next.
	parent := s.parentOf(e, main)
	switch str(e, "type") {
	case "title", "title_change":
		// The conversation's own title, also when omp changes it later. A
		// subagent's file opens with a title too, read before its header
		// says whose it is.
		if rec.Source != feedSubagents && parent == "" && str(e, "title") != "" {
			s.state["title"] = str(e, "title")
		}
		return nil, nil
	case "custom_message":
		if str(e, "customType") == "async-result" && rec.Source == feedSession {
			return s.jobsSettled(e, at)
		}
		return nil, nil
	case "reset_boundary":
		// /clear keeps the conversation and its file but empties the
		// agent's context: say so where it happened.
		var out []harness.Item
		if s.turn != nil && !s.turn.ended {
			out = append(out, s.endTurn(harness.StateOK, at))
		}
		return append(out, harness.Item{ID: "reset-" + str(e, "id"), Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: "Context cleared. The agent no longer sees the messages above."}}), nil
	}
	if parent == dropped {
		return nil, nil
	}
	switch str(e, "type") {
	case "message":
		return s.message(e, parent, at)
	case "model_change":
		if parent == "" {
			return s.modelChange(str(e, "model"), at, str(e, "id"))
		}
	case "thinking_level_change":
		if parent == "" {
			lvl := str(e, "configured")
			if lvl == "" {
				lvl = str(e, "thinkingLevel")
			}
			if lvl != "" {
				s.state["effort"] = lvl
			}
		}
	case "mode_change":
		if parent == "" && str(e, "mode") != "" {
			s.state["mode"] = str(e, "mode")
		}
	case "compaction":
		if parent == "" {
			return s.compaction(e, at)
		}
	case "model_usage":
		// A request beside the turn (19.0.1: the composer's next-message
		// prediction). omp's own spend counts it; so does the status.
		if c, ok := num(obj(obj(e["usage"])["cost"])["total"]); ok && main && parent == "" && c > 0 {
			s.cost += c
			return nil, []harness.Op{s.status()}
		}
	case "custom":
		// /todo in the terminal edits the list by hand and saves the whole
		// list (seen on 18.20: "/todo done" wrote user_todo_edit and the
		// plan stayed 0/2). It replaces the plan like a todo result does.
		if str(e, "customType") == "session_exit" {
			if main {
				return s.exited(at)
			}
			return s.agentExited(parent, str(obj(e["data"]), "kind"), at)
		}
		if parent == "" && str(e, "customType") == "user_todo_edit" {
			if phases, ok := phasesFromDetails(obj(e["data"])); ok {
				s.todo = phases
				return []harness.Item{planFrom("edit-"+str(e, "id"), s.turnID(), at, phases)}, []harness.Op{replacePlan(todoRowsFrom(phases))}
			}
		}
	}
	return nil, nil
}

// feedSession is the feed of the conversation the terminal shows.
const feedSession = "session"

// feedSubagents is the feed of the files omp's subagents write.
const feedSubagents = "subagents"

// header starts a session file: the main session's, or a subagent's. A file
// the session feed reads is the terminal's own conversation whatever its
// header says of a parent: /fork writes a new main conversation that names
// the one it came from (seen on 18.20). A file of the subagents feed, or of
// a mapper fed without feed names, is a subagent's when it names a parent.
func (s *session) header(e map[string]any, at time.Time, main bool, file string) ([]harness.Item, []harness.Op) {
	id := str(e, "id")
	parent := e["parentSession"]
	if main || parent == nil || parent == "" {
		if main && s.state["harness_session"] != "" {
			s.restart()
		}
		s.cur = ""
		if id != "" {
			s.state["harness_session"] = id
			// Naming the conversation lets the engine start the chat over
			// when the terminal moves to another one (/new, /resume, /fork).
			s.state["conversation"] = id
		}
		if t := str(e, "title"); t != "" {
			s.state["title"] = t
		}
		return nil, nil
	}
	return s.subagentHeader(e, at, file)
}

// restart forgets what the main conversation accumulated, for a header
// read again: the file is replayed (a feed restart) or the terminal moved
// to another one. Spend, step counts, the plan and running tools come from
// the records that follow, not from the pass before. Agent rows, which are
// keyed by task id, are kept: their files are read by another feed.
func (s *session) restart() {
	s.turn, s.cost, s.usage = nil, 0, nil
	for id, it := range s.tools {
		if it.Parent == "" {
			delete(s.tools, id)
		}
	}
	for id, it := range s.asks {
		if it.Parent == "" {
			delete(s.asks, id)
		}
	}
	s.jobs = map[string]*harness.Item{}
	s.todo, s.todoBefore, s.todoUnread = nil, map[string][]todoPhase{}, map[string]bool{}
	s.files = map[string]bool{}
	s.byFile.mainAt = time.Time{}
	delete(s.state, "compactions")
}

func (s *session) turnID() string {
	if s.turn == nil {
		return ""
	}
	return s.turn.item.ID
}

func (s *session) message(e map[string]any, parent string, at time.Time) ([]harness.Item, []harness.Op) {
	m := obj(e["message"])
	if m == nil {
		return nil, nil
	}
	if _, ok := e["timestamp"]; !ok {
		at = stamp(m["timestamp"], at)
	}
	id := str(e, "id")
	switch str(m, "role") {
	case "user":
		return s.user(id, parent, text(m["content"]), at)
	case "assistant":
		return s.assistant(id, parent, m, at)
	case "toolResult":
		return s.toolResult(m, at)
	case "bashExecution":
		return s.bash(id, parent, m, at)
	case "developer":
		return s.note(id, parent, at)
	case "compactionSummary", "branchSummary":
		return nil, nil
	}
	return nil, nil
}

func (s *session) user(id, parent, txt string, at time.Time) ([]harness.Item, []harness.Op) {
	if txt == "" {
		return nil, nil
	}
	it := harness.Item{ID: id, Kind: harness.KindUser, At: at, Parent: parent,
		User: &harness.UserBody{Text: clip(txt), Source: "unknown"}}
	if parent != "" {
		return []harness.Item{it}, nil
	}
	var out []harness.Item
	if s.turn != nil && !s.turn.ended {
		out = append(out, s.endTurn(harness.StateOK, at))
	}
	s.turn = &turn{files: map[string]bool{}, item: harness.Item{ID: "turn-" + id, Kind: harness.KindTurn, At: at,
		Turn: &harness.TurnBody{State: harness.StateRunning, Started: at}}}
	s.turn.item.TurnID = s.turn.item.ID
	it.TurnID = s.turn.item.ID
	return append(out, it, s.turn.item), nil
}

func (s *session) assistant(id, parent string, m map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	tid := ""
	if parent == "" && s.turn != nil && s.turn.ended && s.turn.item.Turn.State == harness.StateFailed {
		// omp retries a failed request on its own: the reply that follows
		// the failed one, with no user entry between, continues the work.
		s.turn = &turn{files: map[string]bool{}, item: harness.Item{ID: "turn-" + id, Kind: harness.KindTurn, At: at,
			Turn: &harness.TurnBody{State: harness.StateRunning, Started: at}}}
		s.turn.item.TurnID = s.turn.item.ID
		items = append(items, s.turn.item)
	}
	if parent == "" {
		tid = s.turnID()
		if p, mod := str(m, "provider"), str(m, "model"); mod != "" {
			if p != "" {
				mod = p + "/" + mod
			}
			s.state["model"] = mod
		}
	}
	var texts, thoughts []string
	for _, b := range asList(m["content"]) {
		blk := obj(b)
		switch str(blk, "type") {
		case "text":
			texts = append(texts, str(blk, "text"))
		case "thinking":
			thoughts = append(thoughts, str(blk, "thinking"))
		case "toolCall":
			it, o := s.toolCall(blk, parent, tid, at)
			items, ops = append(items, it...), append(ops, o...)
		}
	}
	if t := joinText(thoughts); t != "" {
		items = append([]harness.Item{{ID: id + "-thinking", TurnID: tid, Parent: parent, Kind: harness.KindThinking, At: at,
			Thinking: &harness.TextBody{Text: clip(t)}}}, items...)
	}
	if t := joinText(texts); t != "" {
		items = append(items, harness.Item{ID: id, TurnID: tid, Parent: parent, Kind: harness.KindAssistant, At: at,
			Assistant: &harness.TextBody{Text: clip(t)}})
	}
	if parent != "" {
		si, so := s.agentStopped(parent, str(m, "stopReason"), at)
		return append(items, si...), append(ops, so...)
	}
	if u := obj(m["usage"]); u != nil {
		s.usage = u
		if c, ok := num(obj(u["cost"])["total"]); ok {
			s.cost += c
		}
		ops = append(ops, s.status())
	}
	if s.turn == nil || s.turn.ended {
		return items, ops
	}
	switch str(m, "stopReason") {
	case "toolUse", "":
	case "aborted":
		items = append(items, s.stopRunning(tid, harness.StateInterrupted, at)...)
		items = append(items, s.endTurn(harness.StateInterrupted, at), harness.Item{ID: id + "-interrupted", TurnID: tid,
			Kind: harness.KindNotice, At: at, Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeInterrupted, Text: "Interrupted"}})
	case "error":
		items = append(items, s.stopRunning(tid, harness.StateFailed, at)...)
		status, _ := num(m["errorStatus"])
		items = append(items, s.endTurn(harness.StateFailed, at), harness.Item{ID: id + "-error", TurnID: tid,
			Kind: harness.KindNotice, At: at, Notice: errtext.Notice(str(m, "errorMessage"), int(status), "The model request failed")})
	default:
		items = append(items, s.endTurn(harness.StateOK, at))
	}
	return items, ops
}

func joinText(parts []string) string {
	out := ""
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue // some models open a tool call with a bare "\n\n"
		}
		if out != "" {
			out += "\n\n"
		}
		out += p
	}
	return out
}

// status is the Status block: model, effort and spend so far.
func (s *session) status() harness.Op {
	st := &harness.StatusBlock{Model: s.state["model"], Effort: s.state["effort"]}
	if s.cost > 0 {
		st.CostUSD = ptr(s.cost)
	}
	if u := s.usage; u != nil {
		in, _ := num(u["input"])
		cr, _ := num(u["cacheRead"])
		out, _ := num(u["output"])
		st.Facts = append(st.Facts, harness.Fact{Label: "Last request", Value: fmt.Sprintf("%.0f in, %.0f cached, %.0f out", in, cr, out)})
	}
	return harness.Op{Op: harness.OpStatus, Status: st}
}

func (s *session) modelChange(model string, at time.Time, id string) ([]harness.Item, []harness.Op) {
	if model == "" || model == s.state["model"] {
		return nil, nil
	}
	prev := s.state["model"]
	s.state["model"] = model
	if prev == "" {
		return nil, []harness.Op{s.status()}
	}
	return []harness.Item{{ID: "model-" + id, TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeModel, Text: "Model " + model}}}, []harness.Op{s.status()}
}

func (s *session) compaction(e map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	n, _ := strconv.Atoi(s.state["compactions"])
	s.state["compactions"] = strconv.Itoa(n + 1)
	txt := "Compacted"
	if m := str(e, "method"); m != "" {
		txt += " with " + m
	}
	before, okB := num(e["tokensBefore"])
	after, okA := num(e["tokensAfter"])
	switch {
	case okB && okA:
		txt += fmt.Sprintf(", %s to %s tokens", short(before), short(after))
	case okB:
		txt += fmt.Sprintf(", from %s tokens", short(before))
	}
	return []harness.Item{{ID: "compaction-" + str(e, "id"), TurnID: s.turnID(), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: txt}}}, nil
}

func short(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

// stopRunning ends the turn's tool calls that never got a result, and
// closes the questions they asked.
func (s *session) stopRunning(tid, state string, at time.Time) []harness.Item {
	out := s.closeAsks(tid)
	for _, it := range s.tools {
		if it.TurnID != tid || it.Tool.State != harness.StateRunning {
			continue
		}
		b := *it.Tool
		b.State, b.Ended = state, ptr(at)
		it.Tool = &b
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
