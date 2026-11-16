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
		Replies: func(_ harness.Env, _ string, off int64) ([]Reply, int64) {
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
		Replies: func(_ harness.Env, _ string, off int64) ([]Reply, int64) {
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
