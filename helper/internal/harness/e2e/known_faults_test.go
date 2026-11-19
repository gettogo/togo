package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestKnownFault_SubagentFilesMatchedByArrival documents a fault: omp's
// mapper gives a subagent file to the task call's first unclaimed task
// (adapters/omp/session.go:256-258, pending[0]), in the order the files
// are read, not by the task the file is for. omp writes the files of a
// batch in the order its agents start, and the engine reads the files
// found in one look in map order (source/file.go:295), so an
// agent's row shows another agent's steps, and which one changes from
// one restore to the next.
//
// Correct: each file goes to its own task (the file is named after it,
// <session>/<task>.jsonl), so Matched finds nothing. This test passes
// while the fault is present; when it fails, the fault is fixed: delete
// the test and the rules it names from known in setup_test.go.
func TestKnownFault_SubagentFilesMatchedByArrival(t *testing.T) {
	e := newEnv(t, 10*time.Minute)
	// Seed 4 writes the six files in an order other than the call's.
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 4, Start: e.start, Cwd: e.cwd,
		Turns: []sessiongen.Turn{{Say: 20, Agents: &sessiongen.Batch{Size: 6, Steps: 1, Background: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	inOrder := true
	for i, a := range l.Truth.Agents {
		inOrder = inOrder && strings.HasPrefix(a.Task, "Task"+strconv.Itoa(i+1)+"x")
	}
	if inOrder {
		t.Fatal("the seed wrote the files in call order; pick another")
	}
	r := e.rig(t, "omp")
	st := r.SettleUntil(10*time.Second, hasPrompt(1))
	vs := enginetest.Matched(st, enginetest.Facts{Truth: l.Truth})
	if len(vs) == 0 {
		t.Fatal("every agent shows its own file's steps: the fault is fixed")
	}
	for _, v := range vs {
		t.Logf("still present: %s", v)
	}
}
