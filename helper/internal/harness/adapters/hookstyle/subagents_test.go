package hookstyle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Payloads shaped as Copilot CLI 1.0.114 sends them
// (copilot/testdata/subagent-hooks.jsonl).
const (
	subParent = `"sessionId":"main-1"`
	subOf     = `,"parentAgentId":"main-1"`
)

func subCall(event, sess, extra string) [2]string {
	return [2]string{event, `{"sessionId":"` + sess + `"` + extra + `}`}
}

// finalStates is the last version of each item by id.
func finalStates(items []harness.Item) map[string]string {
	out := map[string]string{}
	for _, x := range items {
		switch {
		case x.Turn != nil:
			out[x.ID] = x.Turn.State
		case x.Tool != nil:
			out[x.ID] = x.Tool.State
		case x.Agent != nil:
			out[x.ID] = x.Agent.State
		case x.User != nil:
			out[x.ID] = "user:" + x.Parent
		}
	}
	return out
}

func topPrompts(items []harness.Item) int {
	n := 0
	for _, x := range items {
		if x.Kind == harness.KindUser && x.Parent == "" {
			n++
		}
	}
	return n
}

// Two subagents run at once; each one's prompt and stop stay in its own
// item, and the main turn ends once, on the main agent's stop.
func TestTwoSubagentsAtOnce(t *testing.T) {
	items, m := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		[2]string{"preToolUse", `{` + subParent + `,"toolCallId":"c1","toolName":"task","toolArgs":{"description":"One"}}`},
		[2]string{"preToolUse", `{` + subParent + `,"toolCallId":"c2","toolName":"task","toolArgs":{"description":"Two"}}`},
		[2]string{"subagentStart", `{` + subParent + `,"agentId":"a1","agentName":"general-purpose"}`},
		[2]string{"subagentStart", `{` + subParent + `,"agentId":"a2","agentName":"general-purpose"}`},
		subCall("userPromptSubmitted", "a1", subOf+`,"prompt":"one"`),
		subCall("userPromptSubmitted", "a2", subOf+`,"prompt":"two"`),
		subCall("agentStop", "a2", subOf),
		[2]string{"subagentStop", `{` + subParent + `,"agentId":"a2","response":"2"}`},
		subCall("agentStop", "a1", subOf),
		[2]string{"subagentStop", `{` + subParent + `,"agentId":"a1","response":"1"}`},
		[2]string{"postToolUse", `{` + subParent + `,"toolCallId":"c1","toolName":"task","toolResult":{"resultType":"success"}}`},
		[2]string{"postToolUse", `{` + subParent + `,"toolCallId":"c2","toolName":"task","toolResult":{"resultType":"success"}}`},
		[2]string{"agentStop", `{` + subParent + `}`})
	st := finalStates(items)
	if topPrompts(items) != 1 {
		t.Fatalf("%d top-level prompts", topPrompts(items))
	}
	if st["agent-a1"] != harness.StateOK || st["agent-a2"] != harness.StateOK {
		t.Fatalf("agents %v", st)
	}
	for id, s := range st {
		if s == harness.StateInterrupted || s == harness.StateCancelled {
			t.Errorf("%s ended %s", id, s)
		}
	}
	if m.State()["harness_session"] != "main-1" || m.State()["working"] != "false" {
		t.Errorf("state %v", m.State())
	}
}

// A hook of a subagent that has stopped (its agentStop sent late) draws
// nothing and leaves the main turn running.
func TestLateHookOfAFinishedSubagent(t *testing.T) {
	items, m := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		[2]string{"subagentStart", `{` + subParent + `,"agentId":"a1","agentName":"explore"}`},
		[2]string{"subagentStop", `{` + subParent + `,"agentId":"a1"}`},
		subCall("agentStop", "a1", ""))
	st := finalStates(items)
	if st["agent-a1"] != harness.StateOK {
		t.Fatalf("agent %v", st)
	}
	for id, s := range st {
		if id != "agent-a1" && s != harness.StateRunning && s != "user:" {
			t.Errorf("%s is %s", id, s)
		}
	}
	if m.State()["working"] != "true" {
		t.Error("the main turn stopped")
	}
}

// A subagent no subagentStart announced, known only by its parentAgentId,
// gets an item of its own, closed with the turn; its stop does not end
// the turn.
func TestUnannouncedSubagent(t *testing.T) {
	items, _ := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		subCall("userPromptSubmitted", "a9", subOf+`,"prompt":"sub"`),
		subCall("agentStop", "a9", subOf),
		[2]string{"agentStop", `{` + subParent + `}`})
	st := finalStates(items)
	if topPrompts(items) != 1 || st["agent-a9"] != harness.StateOK {
		t.Fatalf("prompts %d, states %v", topPrompts(items), st)
	}
	turns := 0
	for _, x := range items {
		if x.Kind == harness.KindTurn && x.Turn.Ended != nil {
			turns++
			if x.Turn.State != harness.StateOK {
				t.Errorf("turn ended %s", x.Turn.State)
			}
		}
	}
	if turns != 1 {
		t.Errorf("%d turn ends", turns)
	}
}

// Without a parent or an announcement a different session id is not a
// subagent: a nested run or a /new reads as before.
func TestOtherSessionWithoutEvidenceIsNotASubagent(t *testing.T) {
	items, m := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		subCall("userPromptSubmitted", "other", `,"prompt":"nested"`))
	if topPrompts(items) != 2 || m.State()["harness_session"] != "other" {
		t.Fatalf("prompts %d, session %q", topPrompts(items), m.State()["harness_session"])
	}
}

// The helper restarts mid-subagent: the hook store replays the same
// records into a fresh mapper, which draws the same items.
func TestSubagentReplayIsTheSame(t *testing.T) {
	calls := [][2]string{
		{"userPromptSubmitted", `{` + subParent + `,"prompt":"go"}`},
		{"preToolUse", `{` + subParent + `,"toolCallId":"c1","toolName":"task","toolArgs":{"description":"One"}}`},
		{"subagentStart", `{` + subParent + `,"agentId":"a1","agentName":"general-purpose"}`},
		subCall("userPromptSubmitted", "a1", subOf+`,"prompt":"one"`),
		subCall("preToolUse", "a1", subOf+`,"toolCallId":"s1","toolName":"bash","toolArgs":{"command":"ls"}`),
	}
	a, _ := feed(t, "copilot", calls...)
	b, _ := feed(t, "copilot", calls...)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if !reflect.DeepEqual(ja, jb) {
		t.Fatalf("replay differs:\n%s\n%s", ja, jb)
	}
	st := finalStates(a)
	if st["agent-a1"] != harness.StateRunning || st["s1"] != harness.StateRunning {
		t.Fatalf("states %v", st)
	}
	for _, x := range a {
		if x.ID == "s1" && x.Parent != "agent-a1" {
			t.Errorf("subagent tool under %q", x.Parent)
		}
	}
}
