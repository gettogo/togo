//go:build !race

package filefed

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// kimiScript is a long Kimi conversation: tool calls in every turn, a
// Write every seventh turn, a todo list every eleventh, an approval every
// thirteenth (approved, approved for the session, rejected in turn), a
// question every seventeenth, a background job every twentieth, a model
// change every 40, an effort every 33, a mode every 29, plan mode at 50
// and a compaction at 100.
func kimiScript(n, tools, output int) ([]sessiongen.Turn, map[int]sessiongen.KimiTurn, [3]string) {
	extra := map[int]sessiongen.KimiTurn{}
	models := []string{"kimi-code/k3", "qwen5090/qwen3.8-27b", "other/model"}
	efforts := []string{"low", "high"}
	modes := []string{"yolo", "manual", "auto"}
	var final [3]string
	final[0], final[1], final[2] = "qwen5090/qwen3.8-27b", "high", "default"
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		p := i + 1
		var x sessiongen.KimiTurn
		if p%7 == 0 {
			x.Edits = 1 + p%2
		}
		if p%11 == 0 {
			x.Todos = []string{"done", "in_progress", "pending"}[:1+p%3]
		}
		switch {
		case p%13 == 0:
			d := []sessiongen.KimiApproval{
				{Command: "make", Decision: "approved"},
				{Command: "make test", Decision: "approved", Scope: "session"},
				{Command: "make clean", Decision: "rejected", Feedback: "Use make"},
			}[(p/13)%3]
			x.Approval = &d
		case p%17 == 0:
			x.Question = &sessiongen.KimiQuestion{Question: fmt.Sprintf("Which option for step %d?", p), Options: []string{"A", "B", "C"}, Answer: "B"}
		}
		if p%40 == 0 {
			x.Model = models[(p/40)%len(models)]
			final[0] = x.Model
		}
		if p%33 == 0 {
			x.Effort = efforts[(p/33)%len(efforts)]
			final[1] = x.Effort
		}
		if p%29 == 0 {
			x.Mode = modes[(p/29)%len(modes)]
			final[2] = map[string]string{"manual": "default"}[x.Mode]
			if final[2] == "" {
				final[2] = x.Mode
			}
		}
		if p == 50 {
			x.Plan = true
			final[2] = "plan"
		}
		extra[p] = x
		t := sessiongen.Turn{Say: 300, Tools: tools, Output: output, Compact: p == 100}
		if p%20 == 7 {
			t.Jobs = 1
		}
		turns[i] = t
	}
	return turns, extra, final
}

// A long Kimi conversation is followed as Kimi writes it and the engine is
// restarted at five seeded points; every rule holds before and after
// each. The default size crosses the engine's 2000-record replay limit;
// TOGO_E2E_LONG=1 also crosses its 8 MiB read limit.
func TestKimiLongSessionWithRestarts(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/scale/long_session", "kimi/scale/at_limit", "kimi/lifecycle/helper_restart", "kimi/capability/chat")
	e := kimiEnv(t, 48*time.Hour)
	n := 230
	turns, extra, final := kimiScript(n, 3, support.Scale(40, 10_000))
	points := support.Cuts(22, 5, 20, n)
	l, err := sessiongen.Write(e.Home, &sessiongen.Kimi{Extra: extra}, sessiongen.Script{Seed: 22, Start: e.Start, Cwd: e.Cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.Rig(t, "kimi")
	st := support.Drive(t, r, l, turns, points)
	if l.Records[l.Main()] <= 2000 {
		t.Fatalf("%d records, want above the replay limit of 2000", l.Records[l.Main()])
	}
	if fi, err := os.Stat(l.Main()); err != nil || support.LongMode() && fi.Size() <= 8<<20 {
		t.Fatalf("main file %d bytes (%v): want above 8 MiB in long mode", fi.Size(), err)
	}
	last := support.RestoreTwice(t, r, l, n)
	for where, s := range map[string]interface{ Get() harness.StatusBlock }{"restore": status{last}, "following": status{st}} {
		got := s.Get()
		if got.Model != final[0] || got.Effort != final[1] || got.Mode != final[2] {
			t.Errorf("%s: status model %q effort %q mode %q, want %q %q %q", where, got.Model, got.Effort, got.Mode, final[0], final[1], final[2])
		}
	}
	if ok := support.TurnsIn(last, harness.StateOK); len(ok) < 80 {
		t.Errorf("%d finished turns in the restore, want at least 80", len(ok))
	}
	if st.Mission.Summary.NeedsYou != 0 || last.Mission.Summary.NeedsYou != 0 {
		t.Errorf("needs you %d / %d after every question was answered", st.Mission.Summary.NeedsYou, last.Mission.Summary.NeedsYou)
	}
}
