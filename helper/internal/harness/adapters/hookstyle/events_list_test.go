package hookstyle_test

import (
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/adapters/amp"
	"github.com/gettogo/togo/helper/internal/harness/adapters/copilot"
	"github.com/gettogo/togo/helper/internal/harness/adapters/cursor"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The feed drops every event its adapter file does not list, so a failed
// turn's hook must be named there under the canonical name Normalize gives.
func TestFailedTurnEventIsListed(t *testing.T) {
	for _, c := range []struct {
		agent, event, payload string
		file                  []byte
	}{
		{"cursor", "stop", `{"conversation_id":"c1","status":"error"}`, cursor.AdapterFile()},
		{"copilot", "errorOccurred", `{"error":"x","errorContext":"model_call","recoverable":false}`, copilot.AdapterFile()},
		{"copilot", "sessionEnd", `{"reason":"error","error":"x"}`, copilot.AdapterFile()},
		{"amp", "agent.end", `{"thread":{"id":"T-1"},"status":"error"}`, amp.AdapterFile()},
	} {
		d, _ := hookstyle.For(c.agent)
		canon, _, err := hookstyle.Normalize(d, c.event, []byte(c.payload))
		if err != nil || canon != hookstyle.StopFailure {
			t.Fatalf("%s %s: canon %q err %v", c.agent, c.event, canon, err)
		}
		a, err := spec.Parse(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(a.Feeds["hooks"].Hook.Events, canon) {
			t.Errorf("%s: %s is not in the hooks feed's events", c.agent, canon)
		}
	}
}
