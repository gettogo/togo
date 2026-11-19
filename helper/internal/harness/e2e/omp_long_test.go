//go:build !race

// The race detector slows the engine's reading of a session this size
// past a few minutes, so this scenario runs only without it.

package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// longOMP is a two-day-old omp session of 210 turns: four tool calls of
// 10 KB output per turn, a background job every 20 turns, a compaction,
// and six background batches of 5 or 6 subagents (33 files).
func longOMP() []sessiongen.Turn {
	batches := map[int]int{15: 5, 45: 5, 80: 6, 115: 5, 150: 6, 185: 6}
	turns := make([]sessiongen.Turn, 210)
	for i := range turns {
		t := sessiongen.Turn{Say: 300, Tools: 4, Output: 10_000}
		if i%20 == 7 {
			t.Jobs = 1
		}
		if i == 100 {
			t.Compact = true
		}
		if n, ok := batches[i]; ok {
			t.Agents = &sessiongen.Batch{Size: n, Steps: 3, Background: true}
		}
		turns[i] = t
	}
	return turns
}

// The engine follows a long session as the agent writes it and is
// restarted at five points; every rule holds before and after each one.
func TestOMPLongSessionWithRestarts(t *testing.T) {
	e := newEnv(t, 48*time.Hour)
	turns := longOMP()
	points := cuts(11, 5, 20, len(turns))
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 11, Start: e.start, Cwd: e.cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, "omp")
	st := r.SettleUntil(settleLimit, hasPrompt(points[0]))
	check(t, "start", st, l)
	done := points[0]
	for i, p := range append(points[1:], len(turns)) {
		if err := l.Append(turns[done:p]...); err != nil {
			t.Fatal(err)
		}
		done = p
		live := r.SettleUntil(settleLimit, hasPrompt(p))
		check(t, "live "+strconv.Itoa(p), live, l)
		if i == len(points)-1 {
			break // the last append is checked by the restarts below
		}
		r.Restart()
		after := r.SettleUntil(settleLimit, hasPrompt(p))
		check(t, "restart "+strconv.Itoa(p), after, l)
		fail(t, "restart "+strconv.Itoa(p), enginetest.Restarted(live, after))
	}
	if fi, err := os.Stat(l.Main()); err != nil || fi.Size() <= 8<<20 || l.Records[l.Main()] <= 2000 {
		t.Fatalf("main file %d bytes, %d records: want above 8 MiB and 2000", fi.Size(), l.Records[l.Main()])
	}
	if len(l.Subagents) != 33 {
		t.Fatalf("%d subagent files, want 33", len(l.Subagents))
	}
	r.Restart()
	first := r.SettleUntil(settleLimit, hasPrompt(len(turns)))
	check(t, "first restore", first, l)
	r.Restart()
	second := r.SettleUntil(settleLimit, hasPrompt(len(turns)))
	fail(t, "second restore", enginetest.Reread(first, second))
}

// settleLimit bounds each wait: the engine reads a large append to a
// followed file at its own pace.
const settleLimit = 30 * time.Second
