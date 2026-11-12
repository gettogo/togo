package omp

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// toolCall starts a tool item; a task call also announces its subagents
// and a todo call replaces the plan.
func (s *session) toolCall(blk map[string]any, parent, tid string, at time.Time) ([]harness.Item, []harness.Op) {
	callID := str(blk, "id")
	if callID == "" {
		return nil, nil
	}
	name := str(blk, "name")
	args := obj(blk["arguments"])
	info := describeTool(name, args, s.cwd)
	it := &harness.Item{ID: callID, TurnID: tid, Parent: parent, Kind: harness.KindTool, At: at, Tool: &harness.ToolBody{
		CallID: callID, Name: name, Title: info.title, Category: info.category, Subject: info.subject,
		State: harness.StateRunning, Started: at}}
	if info.file != "" {
		it.Tool.Files = []harness.FileChange{{Path: info.file}}
	}
	s.tools[callID] = it
	items := []harness.Item{*it}
	var ops []harness.Op
	if strings.EqualFold(name, "ask") {
		if q := askQuestion(callID, tid, parent, at, args); q != nil {
			s.asks[callID] = q
			items = append(items, *q)
		}
	}
	if parent == "" && s.turn != nil && !s.turn.ended {
		s.turn.item.Turn.Steps++
		if info.file != "" && !s.turn.files[info.file] {
			s.turn.files[info.file] = true
			s.turn.item.Turn.FilesChanged++
		}
		items = append(items, s.turn.item)
	}
	if info.category == harness.ToolAgent && parent == "" {
		for _, t := range taskList(args) {
			aid := "agent-" + firstNonEmpty(t.id, callID+"-"+itoa(len(s.taskCalls[callID])+1))
			a := &harness.Item{ID: aid, TurnID: tid, Kind: harness.KindAgent, At: at, Agent: &harness.AgentBody{
				AgentID: strings.TrimPrefix(aid, "agent-"), Name: truncate(t.title, harness.MaxTitle), Role: t.role,
				State: harness.StateRunning, Started: at}}
			s.agents[aid] = a
			s.taskCalls[callID] = append(s.taskCalls[callID], aid)
			s.pending = append(s.pending, aid)
			items = append(items, *a)
			ops = append(ops, s.agentRow(a))
		}
	}
	if isTodoTool(name) && parent == "" {
		pi, po := s.todoCall(callID, tid, args, at)
		items, ops = append(items, pi...), append(ops, po...)
	}
	return items, ops
}

type task struct{ id, title, role string }

// taskList reads a task call's arguments: a tasks list of {id,
// description, assignment, agent}, or one task given inline.
func taskList(args map[string]any) []task {
	role := str(args, "agent")
	var out []task
	for _, el := range asList(args["tasks"]) {
		m := obj(el)
		t := task{id: str(m, "id"), role: firstNonEmpty(str(m, "agent"), role),
			title: firstNonEmpty(str(m, "description"), firstLine(str(m, "assignment")), str(m, "id"))}
		out = append(out, t)
	}
	if len(out) == 0 {
		out = append(out, task{id: str(args, "id"), role: role,
			title: firstNonEmpty(str(args, "description"), firstLine(firstNonEmpty(str(args, "assignment"), str(args, "task"))), role, "Subagent")})
	}
	return out
}

func (s *session) agentRow(a *harness.Item) harness.Op {
	b := a.Agent
	row := harness.Row{ID: a.ID, Title: b.Name, Subtitle: b.Role, State: b.State, Started: ptr(b.Started),
		Open: &harness.Open{Kind: harness.OpenChat, Ref: a.ID}}
	if b.Summary != "" {
		row.Tail = []string{truncate(b.Summary, harness.MaxTitle)}
	}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}
}

// toolResult finishes a tool item; a task result also finishes its
// subagents.
func (s *session) toolResult(m map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	it, ok := s.tools[str(m, "toolCallId")]
	if !ok {
		return nil, nil
	}
	b := *it.Tool
	b.Ended = ptr(at)
	b.State = harness.StateOK
	if v, _ := m["isError"].(bool); v {
		b.State = harness.StateFailed
	}
	b.Output = tail(text(m["content"]), harness.MaxToolOutput)
	details := obj(m["details"])
	if c, ok := num(details["exitCode"]); ok {
		b.ExitCode = ptr(int(c))
	}
	it.Tool = &b
	items := []harness.Item{*it}
	var ops []harness.Op
	if isTodoTool(b.Name) && it.Parent == "" {
		pi, po := s.todoResult(b.CallID, it.TurnID, m, it.At)
		items, ops = append(items, pi...), append(ops, po...)
	}
	if q, ok := s.asks[b.CallID]; ok {
		body := *q.Question
		body.Answered = askAnswers(&body, m)
		q.Question = &body
		items = append(items, *q)
		delete(s.asks, b.CallID)
	}
	if agents := s.taskCalls[b.CallID]; len(agents) > 0 {
		results := map[string]map[string]any{}
		for _, r := range asList(details["results"]) {
			rm := obj(r)
			results["agent-"+str(rm, "id")] = rm
		}
		for _, aid := range agents {
			a := s.agents[aid]
			ab := *a.Agent
			ab.Ended = ptr(at)
			ab.State = b.State
			if r, ok := results[aid]; ok {
				ab.State = harness.StateOK
				if code, ok := num(r["exitCode"]); ok && code != 0 {
					ab.State = harness.StateFailed
				}
				if v, _ := r["aborted"].(bool); v {
					ab.State = harness.StateCancelled
				}
				ab.Summary = truncate(firstLine(str(r, "output")), harness.MaxTitle)
			}
			a.Agent = &ab
			items = append(items, *a)
			ops = append(ops, s.agentRow(a))
		}
		s.dropPending(agents)
	}
	return items, ops
}

func (s *session) dropPending(ids []string) {
	keep := s.pending[:0]
	for _, p := range s.pending {
		found := false
		for _, id := range ids {
			if p == id {
				found = true
			}
		}
		if !found {
			keep = append(keep, p)
		}
	}
	s.pending = keep
}

// bash maps a command the user ran with "!": a finished shell step of its
// own, outside any turn.
func (s *session) bash(id, parent string, m map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	cmd := str(m, "command")
	if cmd == "" {
		return nil, nil
	}
	info := describeTool("bash", map[string]any{"command": cmd}, s.cwd)
	b := &harness.ToolBody{CallID: id, Name: "bash", Title: info.title, Category: harness.ToolShell, Subject: info.subject,
		State: harness.StateOK, Started: at, Ended: ptr(at), Output: tail(str(m, "output"), harness.MaxToolOutput)}
	if c, ok := num(m["exitCode"]); ok {
		b.ExitCode = ptr(int(c))
		if c != 0 {
			b.State = harness.StateFailed
		}
	}
	if v, _ := m["cancelled"].(bool); v {
		b.State = harness.StateCancelled
	}
	return []harness.Item{{ID: id, Parent: parent, Kind: harness.KindTool, At: at, Tool: b}}, nil
}
