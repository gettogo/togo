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
