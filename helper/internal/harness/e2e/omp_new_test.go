package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// A /new in the middle of a session starts the chat over on the new file:
// a new epoch and none of the old conversation's items. Then omp exits
// with two subagents and nothing else at work: they end with it, and the
// session stops working.
func TestOMPNewConversationThenExit(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/lifecycle/new_conversation", "omp/lifecycle/agent_exits")
	e := newEnv(t, 30*time.Minute)
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 21, Start: e.start, Cwd: e.cwd,
		Turns: []sessiongen.Turn{
			{Say: 40, Tools: 2, Output: 200},
			{Say: 40, Jobs: 1, Agents: &sessiongen.Batch{Size: 3, Steps: 1, Background: true}},
			{Say: 40},
		}})
	if err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, "omp")
	before := r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	check(t, "before /new", before, l)
	old := map[string]bool{}
	for _, it := range before.Items {
		old[it.ID] = true
	}

	if err := l.Append(sessiongen.Turn{New: true, Say: 40, Tools: 1, Output: 100}); err != nil {
		t.Fatal(err)
	}
	after := r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	if after.Epoch == before.Epoch {
		t.Errorf("the chat kept epoch %s after /new", after.Epoch)
	}
	for _, it := range after.Items {
		if old[it.ID] {
			t.Errorf("%s (%s) of the old conversation is still shown after /new", it.ID, it.Kind)
		}
	}
	check(t, "after /new", after, l)

	// Two agents still at work when omp exits.
	if err := l.Append(sessiongen.Turn{Say: 30, Exit: true, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true, Unreported: 2}}); err != nil {
		t.Fatal(err)
	}
	ended := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		if !enginetest.Has(l.Truth.Last)(st) || countKind(st, harness.KindAgent) < 2 {
			return false
		}
		for _, it := range st.Items {
			if it.Agent != nil && it.Agent.State == harness.StateRunning {
				return false
			}
		}
		return true
	})
	check(t, "after exit", ended, l)
	if ended.Mission.Summary.Working || ended.Mission.Summary.Agents != 0 {
		t.Errorf("after the exit the summary is %+v, want nothing working", ended.Mission.Summary)
	}
}

func countKind(st enginetest.State, kind string) int {
	n := 0
	seen := map[string]bool{}
	for _, it := range st.Items {
		if it.Kind == kind && !seen[it.ID] {
			seen[it.ID] = true
			n++
		}
	}
	return n
}
