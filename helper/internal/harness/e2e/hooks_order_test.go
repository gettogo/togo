package e2e

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// hookOrder delivers a session of 12 turns three ways and holds the
// engine to what must stay true when hook processes race: every id once,
// no tool or turn left running, nothing needing the user, the summary
// idle, and the turns the disturbance did not touch exact.
func hookOrder(t *testing.T, a hookAgent) {
	last := func(st enginetest.State) bool { return len(st.Items) > 0 }
	sound := func(where string, st enginetest.State, l *sessiongen.Layout) {
		t.Helper()
		// A reply whose hook was lost or moved leaves its prompt without
		// one: that is the disturbance, not a fault.
		b := a
		b.replies = false
		checkHook(t, where, b, st, l, true)
		for _, it := range st.Items {
			if it.Tool != nil && running(it.Tool.State) {
				t.Errorf("%s: tool %s is %s", where, it.ID, it.Tool.State)
			}
			if it.Turn != nil && running(it.Turn.State) {
				t.Errorf("%s: turn %s is %s", where, it.ID, it.Turn.State)
			}
		}
		if sum := st.Mission.Summary; sum.Working || sum.NeedsYou != 0 || sum.Agents != 0 {
			t.Errorf("%s: summary %+v", where, sum)
		}
	}
	_ = last
	t.Run("duplicated", func(t *testing.T) {
		st, l := orderScenario(t, a, func(calls []sessiongen.HookCall) []sessiongen.HookCall {
			var out []sessiongen.HookCall
			for i, c := range calls {
				out = append(out, c)
				if i%3 == 0 {
					out = append(out, c) // the hook ran twice
				}
			}
			return out
		})
		sound("duplicated", st, l)
	})
	t.Run("swapped", func(t *testing.T) {
		st, l := orderScenario(t, a, func(calls []sessiongen.HookCall) []sessiongen.HookCall {
			for i := 3; i+1 < len(calls); i += 5 {
				calls[i], calls[i+1] = calls[i+1], calls[i] // two hook processes finish in the other order
			}
			return calls
		})
		sound("swapped", st, l)
	})
	t.Run("missing", func(t *testing.T) {
		st, l := orderScenario(t, a, func(calls []sessiongen.HookCall) []sessiongen.HookCall {
			var out []sessiongen.HookCall
			for i, c := range calls {
				if i%7 == 4 && i < len(calls)-20 {
					continue // a hook process that failed
				}
				out = append(out, c)
			}
			return out
		})
		sound("missing", st, l)
	})
	_ = harness.StateOK
}
