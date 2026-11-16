package kimi

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func todoLine(args string) string {
	return `{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"c1","turnId":"0","step":1,"toolCallId":"c1","name":"TodoList","args":` + args + `},"time":1785532203943}`
}

// A TodoList call whose arguments hold no todos list stays a tool step and
// is flagged for the log; an empty list is a plain clear and is not.
func TestUnrecognisedTodoListIsFlagged(t *testing.T) {
	m := NewWire(harness.Env{SessionID: "s1"})
	out := replay(t, m, []string{todoLine(`{"items":[{"title":"x"}]}`)})
	if it := out.items["c1"]; it.Tool == nil || it.Tool.Name != "TodoList" {
		t.Fatalf("no tool step: %+v", out.items)
	}
	if !m.PlanUnrecognised() {
		t.Fatal("not flagged")
	}
	m = NewWire(harness.Env{SessionID: "s1"})
	replay(t, m, []string{todoLine(`{"todos":[]}`)})
	if m.PlanUnrecognised() {
		t.Fatal("empty list flagged")
	}
}

// /new starts a log with a metadata header: the old session's plan, agent
// and background rows must not stay in mission control. An emptied todo
// list clears the plan too.
func TestNewLogAndEmptyTodoListClearTheRows(t *testing.T) {
	m := NewWire(harness.Env{Cwd: "/Users/user/proj", SessionID: "s1"})
	out := replay(t, m, fixture(t, "wire-h1.jsonl"))
	sections := map[string]bool{}
	for _, op := range out.ops {
		if op.Row != nil || len(op.Rows) > 0 {
			sections[op.Section] = true
		}
	}
	if !sections[harness.SectionPlan] || !sections[harness.SectionBackground] {
		t.Fatalf("the recording made no rows: %v", sections)
	}
	_, ops := m.Map(harness.Record{Data: []byte(`{"type":"metadata","protocol_version":"1.4","created_at":1789999634362}`)})
	cleared := map[string]bool{}
	for _, op := range ops {
		if op.Op == harness.OpReplace && len(op.Rows) == 0 {
			cleared[op.Section] = true
		}
	}
	for s := range sections {
		if !cleared[s] {
			t.Errorf("%s rows stay after a new log", s)
		}
	}
	m = NewWire(harness.Env{SessionID: "s1"})
	replay(t, m, []string{todoLine(`{"todos":[{"title":"a","status":"in_progress"}]}`)})
	_, ops = m.Map(harness.Record{Data: []byte(todoLine(`{"todos":[]}`))})
	if len(ops) != 1 || ops[0].Op != harness.OpReplace || ops[0].Section != harness.SectionPlan || len(ops[0].Rows) != 0 {
		t.Fatalf("an emptied list left the plan: %+v", ops)
	}
}
