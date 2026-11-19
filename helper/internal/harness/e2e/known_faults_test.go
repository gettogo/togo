package e2e

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
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
	back := holdFiles(t, l.Subagents)
	r := e.rig(t, "omp")
	r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	back()
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return stepOwners(st) == 6 })
	vs := enginetest.Matched(st, enginetest.Facts{Truth: l.Truth})
	if len(vs) == 0 {
		t.Fatal("every agent shows its own file's steps: the fault is fixed")
	}
	for _, v := range vs {
		t.Logf("still present: %s", v)
	}
}

// TestKnownFault_ReportAfterExitMakesAJob documents a fault: when omp's
// report of a background subagent (an async-result of type task) is read
// after the agent's own file recorded its exit, the mapper finds no
// running agent to settle and makes a finished background job of it
// (adapters/omp/jobs.go:48-63). The Background section then shows the
// subagent a second time, as a job, and whether it does depends on which
// feed was read first, so a restart can drop the row again.
//
// Correct: a task report for an agent that already ended changes nothing.
// This test passes while the fault is present.
func TestKnownFault_ReportAfterExitMakesAJob(t *testing.T) {
	e := newEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 5, Start: e.start, Cwd: e.cwd,
		Turns: []sessiongen.Turn{{Say: 20, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true, ReportLater: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	back := holdFiles(t, l.Subagents)
	r := e.rig(t, "omp")
	r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	back()
	// Both agents ended by their own files before the report is written.
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		if !enginetest.Has(l.Truth.Last)(st) || countKind(st, harness.KindAgent) != 2 {
			return false
		}
		for _, it := range st.Items {
			if it.Agent != nil && it.Agent.State == harness.StateRunning {
				return false
			}
		}
		return true
	})
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	st := r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	var jobs []enginetest.Violation
	for _, v := range enginetest.Ended(st, enginetest.Facts{Truth: l.Truth}) {
		if v.Rule == "agent-shown-as-job" {
			jobs = append(jobs, v)
		}
	}
	if len(jobs) == 0 {
		t.Fatal("no subagent is shown as a job: the fault is fixed")
	}
	for _, v := range jobs {
		t.Logf("still present: %s", v)
	}
}

// holdFiles takes files away and returns a function that puts them back in
// the same order, so a test can have the main conversation read first, as
// when omp writes the subagent files after the task call.
func holdFiles(t *testing.T, paths []string) func() {
	t.Helper()
	bodies := make([][]byte, len(paths))
	for i, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		bodies[i] = b
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		for i, p := range paths {
			if err := os.WriteFile(p, bodies[i], 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestKnownFault_SubagentFileBeforeItsCall documents a fault: a subagent
// file read before the main conversation's task call (the subagents feed
// and the session feed run apart, and after a restart either can come
// first) finds no task waiting. A fresh file becomes a row of its own
// (adapters/omp/session.go:259-264), and the task call later adds the
// task's row as well, which only omp's report ends; a file older than
// five minutes is dropped for good (session.go:248-253), so its agent
// shows no steps.
//
// Correct: one row per task, whichever feed is read first. This test
// passes while the fault is present.
func TestKnownFault_SubagentFileBeforeItsCall(t *testing.T) {
	e := newEnv(t, time.Minute)
	l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 6, Start: e.start, Cwd: e.cwd,
		Turns: []sessiongen.Turn{{Say: 20, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(l.Main())
	if err != nil {
		t.Fatal(err)
	}
	// The conversation as it was before the task call: its first three
	// records (title, header, model).
	head := main[:nthLine(main, 3)]
	if err := os.WriteFile(l.Main(), head, 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, "omp")
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return countKind(st, harness.KindAgent) == 2 })
	if err := os.WriteFile(l.Main(), main, 0o644); err != nil {
		t.Fatal(err)
	}
	st := r.SettleUntil(10*time.Second, enginetest.Has(l.Truth.Last))
	if n := countKind(st, harness.KindAgent); n == 2 {
		t.Fatal("two agent rows for two tasks: the fault is fixed")
	} else {
		t.Logf("still present: %d agent rows for 2 tasks", n)
	}
}

// nthLine is the offset just past the n-th newline.
func nthLine(b []byte, n int) int {
	for i, c := range b {
		if c == '\n' {
			if n--; n == 0 {
				return i + 1
			}
		}
	}
	return len(b)
}
