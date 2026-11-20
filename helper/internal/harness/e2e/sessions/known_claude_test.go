package sessions

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestKnownFault_ClaudeAgentReadBeforeItsCall documents a fault: when a
// subagent's own file is read before the main transcript's Agent call
// (the two are separate feeds, and after a restart the subagents feed can
// win), the mapper makes the agent's row with no turn, and reading the
// call later does not give it one: agentItem returns the existing row
// (adapters/claude/agents.go:15-17) and agentResult takes only the
// description (agents.go:58-75). Which turn shows the agent then depends
// on which feed was read first, so a restart can move it.
//
// Correct: when the call is read, the row takes the call's turn and start
// time. This test passes while the fault is present.
func TestKnownFault_ClaudeAgentReadBeforeItsCall(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, time.Hour)
	l, err := sessiongen.Write(e.Home, &sessiongen.Claude{}, sessiongen.Script{Seed: 32, Start: e.Start, Cwd: e.Cwd,
		Turns: []sessiongen.Turn{{Say: 40, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	// The same transcript, read only after the subagents' files.
	main, err := os.ReadFile(l.Main())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Main(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.Rig(t, "claude")
	hook, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": strings.TrimSuffix(lastPart(l.Main()), ".jsonl"),
		"transcript_path": l.Main(), "source": "startup"})
	r.Deliver("SessionStart", hook)
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return support.CountKind(st, harness.KindAgent) == 2 })
	if err := os.WriteFile(l.Main(), main, 0o644); err != nil {
		t.Fatal(err)
	}
	st := r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	without := 0
	for _, it := range st.Items {
		if it.Agent != nil && it.TurnID == "" {
			without++
			t.Logf("still present: %s has no turn after its Agent call was read", it.ID)
		}
	}
	if without == 0 {
		t.Fatal("every agent row took its call's turn: the fault is fixed")
	}
}
