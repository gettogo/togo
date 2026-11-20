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
)

// TestKnownFault_CopilotSubagentIsTakenForTheMainAgent: the hooks of a
// Copilot CLI subagent (1.0.114, a real turn that starts one with the task
// tool) carry the subagent's own id as sessionId, with agentId and
// parentAgentId beside it. The mapper (hookstyle/mapper.go, UserPromptSubmit
// at line 210) goes by sessionId alone, so:
//   - the subagent's prompt ("Reply with the single word ok", sent to it by
//     the task tool) is a prompt row of the session's own chat;
//   - it opens a new turn, which ends the main turn as interrupted, with
//     the task tool still running at that moment, also interrupted, though
//     both finished normally;
//   - no agent row shows the subagent (the Agents section stays empty).
// The same payloads also lose the turn's final reply: the turn that shows
// is the subagent's, and the main agent's closing message of the live run
// ("done") was not shown. (The reply comes from Copilot's own events log,
// which the capture does not include, so only the three faults above are
// checked here.)
//
// Correct: hooks with a parentAgentId belong to the subagent: no prompt
// row, no interrupt of the main turn, and the subagent in the Agents
// section.
//
// Trigger: any Copilot CLI turn that starts a subagent (task tool).
func TestKnownFault_CopilotSubagentIsTakenForTheMainAgent(t *testing.T) {
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
	st := r.Settle(3 * time.Second)
	prompts := support.UserPrompts(st)
	if len(prompts) != 2 || prompts[1].User.Text != "Reply with the single word ok" || prompts[1].Parent != "" {
		t.Fatalf("the fault changed: prompts %+v, the fault gives the subagent's prompt as the second top-level row", prompts)
	}
	if n := len(support.TurnsIn(st, harness.StateInterrupted)); n != 1 {
		t.Errorf("%d interrupted turns, the fault gives the main turn", n)
	}
	tools := support.Items(st, support.OfKind(harness.KindTool))
	if len(tools) != 1 || tools[0].Tool.Name != "task" || tools[0].Tool.State != harness.StateInterrupted {
		t.Errorf("tool rows %+v, the fault gives the task tool as interrupted", tools)
	}
	if n := support.CountKind(st, harness.KindAgent); n != 0 {
		t.Errorf("%d agent rows, the fault gives none", n)
	}
}
