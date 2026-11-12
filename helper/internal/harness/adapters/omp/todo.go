package omp

import (
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp's todo tool keeps phases of tasks and takes one op per call: init
// (a list of phases), start, done, drop, rm, append and view. Its result
// carries the whole list in details.phases, and the same list as text.
// Task statuses are pending, in_progress, completed and abandoned.

type todoTask struct{ content, status string }
type todoPhase struct {
	name  string
	tasks []todoTask
}

// defaultPhase is the phase omp files tasks under when none is named.
const defaultPhase = "Tasks"

func isTodoTool(name string) bool { return strings.Contains(strings.ToLower(name), "todo") }

func copyPhases(in []todoPhase) []todoPhase {
	out := make([]todoPhase, len(in))
	for i, p := range in {
		out[i] = todoPhase{name: p.name, tasks: append([]todoTask(nil), p.tasks...)}
	}
	return out
}

// todoOp reads a call's op, or infers it as omp does for calls without one.
func todoOp(args map[string]any) string {
	if op := str(args, "op"); op != "" {
		return op
	}
	if len(asList(args["list"])) > 0 {
		return "init"
	}
	if len(asList(args["items"])) > 0 && str(args, "phase") != "" {
		return "append"
	}
	return ""
}

func strList(v any) []string {
	var out []string
	for _, el := range asList(v) {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// applyTodo runs one call's op over a copy of the phases. ok is false when
// the call is not in omp's op format.
func applyTodo(cur []todoPhase, args map[string]any) (out []todoPhase, ok bool) {
	op := todoOp(args)
	out = copyPhases(cur)
	task, phase := str(args, "task"), str(args, "phase")
	find := func() (int, int) {
		for i, p := range out {
			for j, t := range p.tasks {
				if t.content == task {
					return i, j
				}
			}
		}
		return -1, -1
	}
	each := func(f func(*todoTask)) {
		for i := range out {
			if phase != "" && task == "" && out[i].name != phase {
				continue
			}
			for j := range out[i].tasks {
				if task == "" || out[i].tasks[j].content == task {
					f(&out[i].tasks[j])
				}
			}
		}
	}
	switch op {
	case "init":
		out = nil
		list := asList(args["list"])
		if len(list) == 0 && len(strList(args["items"])) > 0 {
			list = []any{map[string]any{"phase": phase, "items": args["items"]}}
		}
		for _, el := range list {
			m := obj(el)
			p := todoPhase{name: firstNonEmpty(str(m, "phase"), defaultPhase)}
			for _, c := range strList(m["items"]) {
				p.tasks = append(p.tasks, todoTask{c, "pending"})
			}
			out = append(out, p)
		}
	case "start":
		i, j := find()
		for a := range out {
			for b := range out[a].tasks {
				if out[a].tasks[b].status == "in_progress" {
					out[a].tasks[b].status = "pending"
				}
			}
		}
		if i >= 0 {
			out[i].tasks[j].status = "in_progress"
		}
	case "done":
		each(func(t *todoTask) { t.status = "completed" })
	case "drop":
		each(func(t *todoTask) { t.status = "abandoned" })
	case "rm":
		for i := range out {
			if phase != "" && task == "" && out[i].name != phase {
				continue
			}
			kept := out[i].tasks[:0]
			for _, t := range out[i].tasks {
				if task != "" && t.content != task {
					kept = append(kept, t)
				}
			}
			out[i].tasks = kept
		}
	case "append":
		if phase == "" && len(out) > 0 {
			phase = out[len(out)-1].name
		}
		phase = firstNonEmpty(phase, defaultPhase)
		i := -1
		for a := range out {
			if out[a].name == phase {
				i = a
			}
		}
		if i < 0 {
			out = append(out, todoPhase{name: phase})
			i = len(out) - 1
		}
		for _, c := range strList(args["items"]) {
			out[i].tasks = append(out[i].tasks, todoTask{c, "pending"})
		}
	case "view":
	default:
		return cur, false
	}
	// omp keeps one task in progress: the first pending one when none is.
	var first *todoTask
	active := false
	for a := range out {
		for b := range out[a].tasks {
			t := &out[a].tasks[b]
			if t.status == "in_progress" {
				active = true
			} else if t.status == "pending" && first == nil {
				first = t
			}
		}
	}
	if !active && first != nil {
		first.status = "in_progress"
	}
	return out, true
}

// phasesFromDetails reads a result's details.phases.
func phasesFromDetails(details map[string]any) ([]todoPhase, bool) {
	list, ok := details["phases"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]todoPhase, 0, len(list))
	for _, el := range list {
		m := obj(el)
		p := todoPhase{name: str(m, "name")}
		for _, t := range asList(m["tasks"]) {
			tm := obj(t)
			c := str(tm, "content")
			if c == "" {
				continue
			}
			p.tasks = append(p.tasks, todoTask{c, normTodoStatus(str(tm, "status"))})
		}
		out = append(out, p)
	}
	return out, true
}

func normTodoStatus(s string) string {
	switch s {
	case "in_progress", "completed", "abandoned":
		return s
	}
	return "pending"
}

var (
	treePhase = regexp.MustCompile(`^ {2}(\S.*):$`)
	treeTask  = regexp.MustCompile(`^ {4}- \[([ xX])\] (.+)$`)
	openTask  = regexp.MustCompile(`^ {2}- (.+) \[(pending|in_progress|completed|abandoned)\] \((.*)\)$`)
)

// phasesFromText reads the list out of a result's text: the per-phase
// checklist when it has one, else the open items it names.
func phasesFromText(txt string) ([]todoPhase, bool) {
	var tree, open []todoPhase
	add := func(list *[]todoPhase, phase string, t todoTask) {
		if n := len(*list); n == 0 || (*list)[n-1].name != phase {
			*list = append(*list, todoPhase{name: phase})
		}
		l := *list
		l[len(l)-1].tasks = append(l[len(l)-1].tasks, t)
	}
	cur, started := "", false
	for _, line := range strings.Split(txt, "\n") {
		if m := openTask.FindStringSubmatch(line); m != nil {
			add(&open, m[3], todoTask{m[1], m[2]})
		} else if strings.HasPrefix(line, "Active phase ") {
			started = true
		} else if m := treePhase.FindStringSubmatch(line); m != nil && started {
			cur = m[1]
			tree = append(tree, todoPhase{name: cur})
		} else if m := treeTask.FindStringSubmatch(line); m != nil && started && cur != "" {
			c, st := m[2], "pending"
			switch {
			case m[1] != " ":
				st = "completed"
			case strings.HasSuffix(c, " (in progress)"):
				c, st = strings.TrimSuffix(c, " (in progress)"), "in_progress"
			case strings.HasSuffix(c, " (dropped)"):
				c, st = strings.TrimSuffix(c, " (dropped)"), "abandoned"
			}
			tree[len(tree)-1].tasks = append(tree[len(tree)-1].tasks, todoTask{c, st})
		}
	}
	if len(tree) > 0 {
		return tree, true
	}
	return open, len(open) > 0
}

func closed(s string) bool { return s == "completed" || s == "abandoned" }

// todoRowsFrom turns phases into plan rows: one parent row per phase with
// its tasks as children, or flat rows for a lone default phase.
func todoRowsFrom(phases []todoPhase) []harness.Row {
	var rows []harness.Row
	flat := len(phases) == 1 && phases[0].name == defaultPhase
	for i, p := range phases {
		if len(p.tasks) == 0 {
			continue
		}
		var kids []harness.Row
		done, active := 0, false
		for j, t := range p.tasks {
			st := rowState(t.status)
			if closed(t.status) {
				done++
			}
			active = active || st == harness.RowActive
			kids = append(kids, harness.Row{ID: "todo-" + itoa(i+1) + "-" + itoa(j+1), Title: truncate(t.content, harness.MaxTitle), State: st})
		}
		if flat {
			return kids
		}
		st := harness.RowPending
		switch {
		case done == len(p.tasks):
			st = harness.RowDone
		case active || done > 0:
			st = harness.RowActive
		}
		prog := float64(done) / float64(len(p.tasks))
		rows = append(rows, harness.Row{ID: "todo-" + itoa(i+1), Title: truncate(p.name, harness.MaxTitle), State: st, Progress: &prog, Children: kids})
	}
	return rows
}

// planFrom counts the tasks (not the phases) for the plan marker.
func planFrom(callID, tid string, at time.Time, phases []todoPhase) harness.Item {
	p := &harness.PlanBody{}
	for _, ph := range phases {
		for _, t := range ph.tasks {
			p.Total++
			if closed(t.status) {
				p.Done++
			}
			if t.status == "in_progress" && p.Current == "" {
				p.Current = truncate(t.content, harness.MaxTitle)
			}
		}
	}
	return harness.Item{ID: "plan-" + callID, TurnID: tid, Kind: harness.KindPlan, At: at, Plan: p}
}

func replacePlan(rows []harness.Row) harness.Op {
	return harness.Op{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}
}

// todoCall reads a main-session todo call: omp's op format over the plan
// so far, else the older flat shapes. A call neither reads is held until
// its result, which may still carry the list.
func (s *session) todoCall(callID, tid string, args map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	if next, ok := applyTodo(s.todo, args); ok {
		if todoOp(args) == "view" {
			return nil, nil
		}
		s.todoBefore[callID] = s.todo
		s.todo = next
		return []harness.Item{planFrom(callID, tid, at, next)}, []harness.Op{replacePlan(todoRowsFrom(next))}
	}
	if rows := todoRows(args); len(rows) > 0 {
		s.todoBefore[callID] = s.todo
		s.todo = nil
		p := &harness.PlanBody{Total: len(rows)}
		for _, r := range rows {
			if r.State == harness.RowDone || r.State == harness.RowSkipped {
				p.Done++
			} else if r.State == harness.RowActive && p.Current == "" {
				p.Current = r.Title
			}
		}
		return []harness.Item{{ID: "plan-" + callID, TurnID: tid, Kind: harness.KindPlan, At: at, Plan: p}}, []harness.Op{replacePlan(rows)}
	}
	s.todoUnread[callID] = true
	return nil, nil
}

// todoResult takes the list omp returns as the truth: details.phases, else
// its text. A failed op leaves the plan as it was before the call.
func (s *session) todoResult(callID, tid string, m map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	unread := s.todoUnread[callID]
	delete(s.todoUnread, callID)
	before, had := s.todoBefore[callID]
	delete(s.todoBefore, callID)
	failed, _ := m["isError"].(bool)
	if failed {
		if !had {
			return nil, nil
		}
		s.todo = before
		return []harness.Item{planFrom(callID, tid, at, before)}, []harness.Op{replacePlan(todoRowsFrom(before))}
	}
	phases, ok := phasesFromDetails(obj(m["details"]))
	if !ok {
		phases, ok = phasesFromText(text(m["content"]))
	}
	if !ok {
		if unread {
			s.NoteUnrecognisedPlan()
		}
		return nil, nil
	}
	s.todo = phases
	return []harness.Item{planFrom(callID, tid, at, phases)}, []harness.Op{replacePlan(todoRowsFrom(phases))}
}
