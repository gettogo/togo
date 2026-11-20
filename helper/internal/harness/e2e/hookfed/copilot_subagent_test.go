package hookfed

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

// TestCopilotSubagentStaysInsideItsTurn: the hooks of a Copilot CLI
// subagent (1.0.114, a real turn that starts one with the task tool) carry
// the subagent's own id as sessionId, with agentId and parentAgentId
// beside it. They belong to the subagent: its prompt ("Reply with the
// single word ok", sent to it by the task tool) is drawn inside its agent
// item, not as a prompt of the chat; its stop does not end the main turn;
// the task tool ends ok; and the subagent shows as one agent, finished.
// Before the fix (hookstyle/subagents.go) the mapper went by sessionId
// alone and the subagent's prompt opened a new turn that interrupted the
// main one.
func TestCopilotSubagentStaysInsideItsTurn(t *testing.T) {
	t.Parallel()
	f, err := os.Open("../../adapters/copilot/testdata/subagent-hooks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	e := support.NewEnv(t, time.Hour)
	r := e.Rig(t, "copilot")
	d, _ := hookstyle.For("copilot")
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var rec struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Event == "sessionEnd" {
			continue // the user's exit of Copilot ends the session, not the turn
		}
		var p struct {
			Timestamp int64 `json:"timestamp"`
		}
		_ = json.Unmarshal(rec.Payload, &p)
		fw, err := hookstyle.Reduce(d, rec.Event, rec.Payload, support.HookEnv(r))
		if err != nil {
			t.Fatalf("%s: %v", rec.Event, err)
		}
		r.DeliverAt(fw.Hook, fw.Payload, time.UnixMilli(p.Timestamp))
	}
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		return len(support.TurnsIn(st, harness.StateOK)) == 1
	})
	prompts := support.UserPrompts(st)
	if len(prompts) != 1 || prompts[0].User.Text == "Reply with the single word ok" {
		t.Errorf("top-level prompts %+v, want only the user's", prompts)
	}
	agents := support.Items(st, support.OfKind(harness.KindAgent))
	if len(agents) != 1 || agents[0].Agent.State != harness.StateOK {
		t.Fatalf("agent items %+v, want one finished subagent", agents)
	}
	inner := support.Items(st, func(it harness.Item) bool { return it.User != nil && it.Parent == agents[0].ID })
	if len(inner) != 1 || inner[0].User.Text != "Reply with the single word ok" {
		t.Errorf("subagent prompts %+v, want its task prompt inside it", inner)
	}
	if n := len(support.TurnsIn(st, harness.StateInterrupted)); n != 0 {
		t.Errorf("%d interrupted turns, want none", n)
	}
	tools := support.Items(st, support.OfKind(harness.KindTool))
	if len(tools) != 1 || tools[0].Tool.Name != "task" || tools[0].Tool.State != harness.StateOK {
		t.Errorf("tool rows %+v, want the task tool ended ok", tools)
	}
	if st.Mission.Summary.Agents != 0 {
		t.Errorf("summary counts %d running agents after the turn", st.Mission.Summary.Agents)
	}
}
