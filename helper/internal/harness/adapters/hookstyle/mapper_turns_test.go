package hookstyle

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Cursor payloads, shaped as in testdata/cursor.jsonl.
const (
	curPrompt = `{"conversation_id":"c7a1-0001","generation_id":"g-01","session_id":"c7a1-0001","hook_event_name":"beforeSubmitPrompt","prompt":"list the files"}`
	curPre    = `{"conversation_id":"c7a1-0001","generation_id":"g-01","session_id":"c7a1-0001","hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"ls"},"tool_use_id":"tool_01","cwd":"/HOME/proj"}`
	curPost   = `{"conversation_id":"c7a1-0001","generation_id":"g-01","session_id":"c7a1-0001","hook_event_name":"postToolUse","tool_name":"Shell","tool_input":{"command":"ls"},"tool_output":"main.go","tool_use_id":"tool_01","cwd":"/HOME/proj"}`
)

// last returns the final state of each item id.
func last(items []harness.Item) map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, x := range items {
		out[x.ID] = x
	}
	return out
}

func TestPromptWhileTurnRunsClosesTheOldTurn(t *testing.T) {
	items, m := feed(t, "cursor",
		[2]string{"beforeSubmitPrompt", curPrompt},
		[2]string{"preToolUse", curPre},
		[2]string{"beforeSubmitPrompt", curPrompt},
		[2]string{"preToolUse", curPre})
	var turns []harness.Item
	for _, x := range last(items) {
		if x.Kind == harness.KindTurn {
			turns = append(turns, x)
		}
	}
	if len(turns) != 2 {
		t.Fatalf("turns %d", len(turns))
	}
	open := 0
	for _, x := range turns {
		if x.Turn.Ended == nil {
			open++
		} else if x.Turn.State != harness.StateInterrupted {
			t.Errorf("closed turn %q", x.Turn.State)
		}
	}
	if open != 1 {
		t.Fatalf("%d turns still running", open)
	}
	if m.State()["working"] != "true" {
		t.Fatal("new turn not working")
	}
}

// A hook that no reply message can match must not wait for one: Map runs
// under the session's lock.
func TestRepliesDoNotWaitForWhatCannotArrive(t *testing.T) {
	oldTool, oldPoll := replyWaitTool, replyWaitStop
	replyWaitTool, replyWaitStop = 400*time.Millisecond, 400*time.Millisecond
	defer func() { replyWaitTool, replyWaitStop = oldTool, oldPoll }()
	reads := 0
	m := New(harness.Env{SessionID: "s-wait"}, Options{Name: "copilot.hooks",
		Replies: func(_ harness.Env, _ string, off, _ int64) ([]Reply, int64) {
			if off < 0 {
				return nil, 5
			}
			reads++
			if reads == 1 { // one assistant message requesting two tools
				return []Reply{{Calls: []string{"call-a", "call-b"}}}, 10
			}
			return nil, 10
		}})
	hook := func(at int, body string) time.Duration {
		d, _ := For("copilot")
		_, norm, err := Normalize(d, "preToolUse", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		m.Map(harness.Record{Source: "hooks", At: t0.Add(time.Duration(at) * time.Second), Data: norm})
		return time.Since(start)
	}
	m.Map(harness.Record{At: t0, Data: []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"copilot-session-1","prompt":"hi"}`)})
	hook(1, `{"session_id":"copilot-session-1","toolName":"bash","toolCallId":"call-a","toolArgs":"{}"}`)
	if d := hook(2, `{"session_id":"copilot-session-1","toolName":"bash","toolCallId":"call-b","toolArgs":"{}"}`); d > 100*time.Millisecond {
		t.Errorf("a call of a message already read waited %v", d)
	}
	if d := hook(3, `{"session_id":"copilot-session-1","toolName":"bash","toolArgs":"{}"}`); d > 100*time.Millisecond {
		t.Errorf("a call without an id waited %v", d)
	}
}

// After a helper restart the first hook the mapper sees is a tool call:
// the log's earlier messages are not this turn's.
func TestRepliesFromTheEndWhenTheSessionIsNew(t *testing.T) {
	m := New(harness.Env{SessionID: "s-restart"}, Options{Name: "copilot.hooks",
		Replies: func(_ harness.Env, _ string, off, _ int64) ([]Reply, int64) {
			if off < 0 {
				return nil, 900
			}
			if off == 0 {
				return []Reply{{Text: "an answer from long ago"}}, 900
			}
			return nil, off
		}})
	d, _ := For("copilot")
	_, norm, _ := Normalize(d, "preToolUse", []byte(`{"session_id":"copilot-session-1","toolName":"bash","toolCallId":"c1","toolArgs":"{}"}`))
	items, _ := m.Map(harness.Record{Source: "hooks", At: t0, Data: norm})
	for _, x := range items {
		if x.Kind == harness.KindAssistant {
			t.Fatalf("replayed %q", x.Assistant.Text)
		}
	}
}

// Agent rows carry run states, which the mission summary counts.
func TestSubagentRowsCarryRunStates(t *testing.T) {
	d, _ := For("cursor")
	var states []string
	m := New(harness.Env{SessionID: "s-agents"}, Options{Name: "cursor.hooks"})
	for i, e := range []string{"subagentStart", "subagentStop"} {
		_, norm, err := Normalize(d, e, []byte(`{"conversation_id":"c1","session_id":"c1","subagent_id":"a1","subagent_type":"explore"}`))
		if err != nil {
			t.Fatal(err)
		}
		_, ops := m.Map(harness.Record{At: t0.Add(time.Duration(i) * time.Second), Data: norm})
		for _, op := range ops {
			states = append(states, op.Row.State)
		}
	}
	if len(states) != 2 || states[0] != harness.StateRunning || states[1] != harness.StateOK {
		t.Fatalf("states %v", states)
	}
}

// Copilot's subagent hooks carry the agent's name and no id: two agents of
// one type get a row each, and a turn that ends closes what never stopped.
func TestSubagentRowsAreKeyedPerAgentAndClosedWithTheTurn(t *testing.T) {
	d, _ := For("copilot")
	m := New(harness.Env{SessionID: "s-sub"}, Options{Name: "copilot.hooks"})
	rows := map[string]string{}
	step := func(i int, event, body string) {
		_, norm, err := Normalize(d, event, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		_, ops := m.Map(harness.Record{At: t0.Add(time.Duration(i) * time.Second), Data: norm})
		for _, op := range ops {
			if op.Section == harness.SectionAgents {
				rows[op.Row.ID] = op.Row.State
			}
		}
	}
	step(0, "userPromptSubmitted", `{"prompt":"go"}`)
	step(1, "subagentStart", `{"agentName":"explore"}`)
	step(2, "subagentStart", `{"agentName":"explore"}`)
	step(3, "subagentStop", `{"agentName":"explore"}`)
	if len(rows) != 2 || rows["explore"] != harness.StateOK || rows["explore#2"] != harness.StateRunning {
		t.Fatalf("after one stop: %v", rows)
	}
	step(4, "userPromptSubmitted", `{"prompt":"go on"}`) // interrupts the turn
	if rows["explore#2"] != harness.StateCancelled {
		t.Fatalf("after the turn ended: %v", rows)
	}
}

// Copilot reports a fatal error and then the session's end with the same
// error: the notice is drawn once.
func TestOneNoticeForAFailureReportedTwice(t *testing.T) {
	items, _ := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{"prompt":"hi"}`},
		[2]string{"errorOccurred", `{"error":{"message":"Request failed with status 401: Unauthorized","name":"CAPIError"},"errorContext":"model_call","recoverable":false}`},
		[2]string{"sessionEnd", `{"reason":"error","error":"Request failed with status 401: Unauthorized"}`})
	n := 0
	for _, x := range items {
		if x.Notice != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d notices", n)
	}
}

// Copilot names agents by type only. A third agent of a type, started after
// the first stopped while the second still runs, gets its own row: it does
// not share the second's, which a later stop would mark finished for both.
func TestAThirdAgentOfATypeDoesNotShareARunningRow(t *testing.T) {
	d, _ := For("copilot")
	m := New(harness.Env{SessionID: "s-sub3"}, Options{Name: "copilot.hooks"})
	rows := map[string]string{}
	step := func(i int, event, body string) {
		_, norm, err := Normalize(d, event, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		_, ops := m.Map(harness.Record{At: t0.Add(time.Duration(i) * time.Second), Data: norm})
		for _, op := range ops {
			if op.Section == harness.SectionAgents {
				rows[op.Row.ID] = op.Row.State
			}
		}
	}
	step(0, "userPromptSubmitted", `{"prompt":"go"}`)
	step(1, "subagentStart", `{"agentName":"explore"}`)
	step(2, "subagentStart", `{"agentName":"explore"}`)
	step(3, "subagentStop", `{"agentName":"explore"}`)
	step(4, "subagentStart", `{"agentName":"explore"}`)
	if len(rows) != 3 || rows["explore#2"] != harness.StateRunning || rows["explore#3"] != harness.StateRunning {
		t.Fatalf("after a third start: %v", rows)
	}
	step(5, "subagentStop", `{"agentName":"explore"}`)
	if rows["explore#2"] != harness.StateOK || rows["explore#3"] != harness.StateRunning {
		t.Fatalf("after the second stop: %v", rows)
	}
}
