package turns

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Crush adds the finish part to an assistant row after the result row of
// its first tool exists, so the row is mapped again with every call in it.
func TestRewrittenMessageDoesNotRestartAFinishedCall(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	b := New()
	b.User("u1", "run two things", at)
	args := json.RawMessage(`{"command":"ls"}`)
	b.ToolStart("call-1", "bash", args, at)
	b.ToolStart("call-2", "bash", args, at)
	if x, ok := b.ToolEnd("call-1", "main.go", harness.StateOK, at.Add(time.Second)); !ok || x.Tool.State != harness.StateOK {
		t.Fatalf("end %+v", x)
	}
	again := b.ToolStart("call-1", "bash", args, at.Add(2*time.Second))
	if again.Tool.State != harness.StateOK || again.Tool.Output != "main.go" {
		t.Fatalf("a finished call started again: %+v", again.Tool)
	}
	items := b.Close(harness.StateOK, at.Add(3*time.Second))
	for _, x := range items {
		if x.ID == "call-1" {
			t.Fatalf("a finished call closed again: %+v", x.Tool)
		}
	}
	if b.TurnItem(harness.StateOK, nil).Turn.Steps != 2 {
		t.Fatalf("steps %d", b.TurnItem(harness.StateOK, nil).Turn.Steps)
	}
}
