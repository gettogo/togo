package omp

import (
	"fmt"
	"sort"
	"strconv"
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
	env harness.Env
	cwd string

	mu        sync.Mutex
	state     map[string]string
	dirAt     time.Time
	parents   map[string]string // entry id -> agent item id ("" for the main session)
	cur       string            // parent of the file read last
	turn      *turn
	tools     map[string]*harness.Item
	taskCalls map[string][]string // task call id -> agent item ids
	pending   []string            // agent item ids still waiting for their file
	agents    map[string]*harness.Item
	headers   map[string]string // subagent header id -> agent item id
	files     map[string]bool
	cost      float64
	usage     map[string]any
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
		tools: map[string]*harness.Item{}, taskCalls: map[string][]string{}, agents: map[string]*harness.Item{},
		headers: map[string]string{}, files: map[string]bool{}}
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
	out := make(map[string]string, len(s.state)+2)
	for k, v := range s.state {
		out[k] = v
	}
	if s.turn != nil {
		out["turn"] = s.turn.item.ID
		out["working"] = strconv.FormatBool(!s.turn.ended)
	}
	return out
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
	at := stamp(e["timestamp"], rec.At)
	switch str(e, "type") {
	case "title":
		if s.cur == "" && str(e, "title") != "" {
			s.state["title"] = str(e, "title")
		}
		return nil, nil
	case "session":
		return s.header(e, at)
	}
	parent := s.parentOf(e)
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
	}
	return nil, nil
}

// header starts a session file: the main session's, or a subagent's when
// it names a parent session.
func (s *session) header(e map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	id := str(e, "id")
	parent := e["parentSession"]
	if parent == nil || parent == "" {
		s.cur = ""
		if id != "" {
			s.state["harness_session"] = id
		}
		if t := str(e, "title"); t != "" {
			s.state["title"] = t
		}
		return nil, nil
	}
	if a, ok := s.headers[id]; ok {
		s.cur = a
		return nil, nil
	}
	var agentID string
	if created, err := time.Parse(time.RFC3339, s.env.Vars["created"]); err == nil && len(s.pending) == 0 && at.Before(created.Add(-time.Minute)) {
		// A subagent of an earlier session, read because its file was
		// the newest: not part of this conversation.
		s.headers[id], s.cur = dropped, dropped
		return nil, nil
	}
	if len(s.pending) > 0 {
		agentID, s.pending = s.pending[0], s.pending[1:]
	} else {
		agentID = "agent-" + id
		it := harness.Item{ID: agentID, Kind: harness.KindAgent, At: at, TurnID: s.turnID(),
			Agent: &harness.AgentBody{AgentID: id, Name: firstNonEmpty(str(e, "title"), "Subagent"), State: harness.StateRunning, Started: at}}
		s.agents[agentID] = &it
	}
	s.headers[id] = agentID
	s.cur = agentID
	it := *s.agents[agentID]
	return []harness.Item{it}, []harness.Op{s.agentRow(&it)}
}

func (s *session) parentOf(e map[string]any) string {
	if p := str(e, "parentId"); p != "" {
		if o, ok := s.parents[p]; ok {
			s.parents[str(e, "id")] = o
			return o
		}
	}
	if id := str(e, "id"); id != "" {
		s.parents[id] = s.cur
	}
	return s.cur
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

func (s *session) endTurn(state string, at time.Time) harness.Item {
	t := s.turn
	t.ended = true
	t.item.Turn.State = state
	t.item.Turn.Ended = ptr(at)
	return t.item
}

func (s *session) assistant(id, parent string, m map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	tid := ""
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
		return items, ops
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
		if p == "" {
			continue
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

// stopRunning ends the turn's tool calls that never got a result.
func (s *session) stopRunning(tid, state string, at time.Time) []harness.Item {
	var out []harness.Item
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
