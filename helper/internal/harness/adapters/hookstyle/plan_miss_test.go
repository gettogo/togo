package hookstyle

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func hook(m *Mapper, payload string) {
	m.Map(harness.Record{Source: "hooks", At: t0, Data: json.RawMessage(payload)})
}

// Todo tool calls are not read by this mapper, only the task hooks. The
// call stays a tool step and is flagged for the log, unless task hooks
// already fed the plan.
func TestTodoToolCallIsFlaggedUnlessTaskHooksFeedThePlan(t *testing.T) {
	m := New(harness.Env{SessionID: "s_todo00001"}, Options{Name: "droid.hooks"})
	hook(m, `{"hook_event_name":"UserPromptSubmit","session_id":"a","prompt":"go"}`)
	hook(m, `{"hook_event_name":"PreToolUse","session_id":"a","tool_name":"TodoWrite","tool_input":{"todos":"1. [pending] x"}}`)
	if !m.PlanUnrecognised() {
		t.Fatal("todo tool call not flagged")
	}

	m = New(harness.Env{SessionID: "s_todo00002"}, Options{Name: "droid.hooks"})
	hook(m, `{"hook_event_name":"UserPromptSubmit","session_id":"a","prompt":"go"}`)
	hook(m, `{"hook_event_name":"TaskCreated","session_id":"a","task_id":"1","task_subject":"x"}`)
	hook(m, `{"hook_event_name":"PreToolUse","session_id":"a","tool_name":"TodoWrite","tool_input":{}}`)
	if m.PlanUnrecognised() {
		t.Fatal("flagged although task hooks feed the plan")
	}
	m = New(harness.Env{SessionID: "s_todo00003"}, Options{Name: "droid.hooks"})
	hook(m, `{"hook_event_name":"PreToolUse","session_id":"a","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	if m.PlanUnrecognised() {
		t.Fatal("a shell call flagged")
	}
}
