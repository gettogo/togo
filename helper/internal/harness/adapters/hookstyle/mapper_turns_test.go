package hookstyle

import (
	"testing"

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
