package enginetest

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// A short omp session read through the real engine holds its turns and
// agents, passes every rule, and reads the same after a restart.
func TestRigServesGeneratedOMPSession(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	cwd := filepath.Join(home, "proj")
	start := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	l, err := sessiongen.Write(home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 3, Start: start, Cwd: cwd,
		Turns: []sessiongen.Turn{
			{Say: 50, Tools: 2, Output: 100},
			{Agents: &sessiongen.Batch{Size: 2, Steps: 1}, Say: 30},
			{Jobs: 1, Say: 20},
		}})
	if err != nil {
		t.Fatal(err)
	}
	r := New(t, Options{Agent: "omp", Home: home, Cwd: cwd, Created: start.Add(-30 * time.Second)})
	st := r.SettleUntil(10*time.Second, func(st State) bool { return countKind(st, harness.KindTurn) == 3 })
	f := Facts{Truth: l.Truth, Over: true}
	Fail(t, "first read", Check(st, f))
	if n := countKind(st, harness.KindAgent); n != 2 {
		t.Errorf("%d agent items, want 2", n)
	}
	small := r.ReadPages(3)
	Fail(t, "pages of 3", Paging(small))
	Fail(t, "paging", SamePaging(st, small))
	r.Restart()
	again := r.SettleUntil(10*time.Second, func(st State) bool { return countKind(st, harness.KindTurn) == 3 })
	Fail(t, "after restart", Check(again, f))
	Fail(t, "restart", Restarted(st, again))
}

func countKind(st State, kind string) int {
	n := 0
	for _, it := range latest(st.Items) {
		if it.Kind == kind {
			n++
		}
	}
	return n
}
