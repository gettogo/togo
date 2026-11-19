package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// longOMP is a two-day-old omp session of n turns: four tool calls of
// 10 KB output per turn, a background job every 20 turns, a compaction,
// and background batches of 5 or 6 subagents.
func longOMP(n int) []sessiongen.Turn {
	sizes := []int{5, 5, 6, 5, 6, 6}
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		t := sessiongen.Turn{Say: 300, Tools: 4, Output: 10_000}
		if i%20 == 7 {
			t.Jobs = 1
		}
		if i == n/2 {
			t.Compact = true
		}
		if k := i / 35; i%35 == 15 && k < len(sizes) {
			t.Agents = &sessiongen.Batch{Size: sizes[k], Steps: 3, Background: true}
		}
		turns[i] = t
	}
	return turns
}

// The long scenario's size. The default crosses every restore limit
// (main file over 8 MiB and 2,000 records, six subagent batches, 33
// files) with two restart points; TOGO_E2E_LONG=1 adds three more.
func longPoints() int {
	if os.Getenv("TOGO_E2E_LONG") != "" {
		return 5
	}
	return 2
}

// The engine follows a long session as the agent writes it. At each
// point the helper restarts on the same files and must show the same
// chat; then it is down while the agent works on, and the next engine
// restores what was written meanwhile. Every rule holds throughout.
func TestOMPLongSessionWithRestarts(t *testing.T) {
	coverage.Covers(t, "generated", "omp/scale/long_session", "omp/lifecycle/helper_restart",
		"omp/capability/agents", "omp/capability/background")
	e := newEnv(t, 48*time.Hour)
	turns := longOMP(210)
	points := append(cuts(11, longPoints(), 20, len(turns)-3), len(turns)-3)
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 11, Start: e.start, Cwd: e.cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.rigWith(t, enginetest.Options{Agent: "omp", NoRedact: true})
	check(t, "start", r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last)), l)
	done := points[0]
	for i, p := range points {
		if i > 0 {
			// The helper is down while the agent works up to p.
			r.Stop()
			if err := l.Append(turns[done:p]...); err != nil {
				t.Fatal(err)
			}
			r.Start()
			check(t, "restored "+strconv.Itoa(p), r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last)), l)
		}
		// The agent keeps working while the helper follows it.
		if err := l.Append(turns[p : p+3]...); err != nil {
			t.Fatal(err)
		}
		done = p + 3
		live := r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last))
		check(t, "live "+strconv.Itoa(done), live, l)
		r.Restart()
		after := r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last))
		check(t, "restart "+strconv.Itoa(done), after, l)
		fail(t, "restart "+strconv.Itoa(done), enginetest.Restarted(live, after), l)
	}
	if fi, err := os.Stat(l.Main()); err != nil || fi.Size() <= 8<<20 || l.Records[l.Main()] <= 2000 {
		t.Fatalf("main file %d bytes, %d records: want above 8 MiB and 2000", fi.Size(), l.Records[l.Main()])
	}
	if len(l.Subagents) != 33 {
		t.Fatalf("%d subagent files, want 33", len(l.Subagents))
	}
	r.Restart()
	second := r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last))
	r.Restart()
	third := r.SettleUntil(settleLimit, enginetest.Has(l.Truth.Last))
	fail(t, "second restore", enginetest.Reread(second, third), l)
}

// settleLimit bounds each wait.
const settleLimit = 60 * time.Second
