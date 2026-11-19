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

// TestSubagentFilesMatchedByName: omp writes the files of a batch in the
// order its agents start, and the engine reads the files found in one
// look in no fixed order, so the order files arrive in says nothing of
// whose they are. Each file goes to the task it is named after
// (<session>/<task>.jsonl), and every agent shows its own file's steps.
func TestSubagentFilesMatchedByName(t *testing.T) {
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
	enginetest.Fail(t, "after the files", enginetest.Matched(st, enginetest.Facts{Truth: l.Truth}))
}

// TestReportAfterExitKeepsTheAgentRow: omp's report of a background
// subagent (an async-result of type task) read after the agent's own file
// recorded its exit leaves the agent's row as it ended, and the subagent
// is not shown a second time as a background job.
func TestReportAfterExitKeepsTheAgentRow(t *testing.T) {
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
	enginetest.Fail(t, "after the report", enginetest.Ended(st, enginetest.Facts{Truth: l.Truth}))
	if n := countKind(st, harness.KindBackground); n != 0 {
		t.Fatalf("%d background jobs for 2 subagents", n)
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

// TestSubagentFileBeforeItsCall: the subagents feed and the session feed
// run apart, and after a restart either can come first. A subagent file
// read before the main conversation's task call waits for the call, then
// goes to its task: one row per task, each with its own file's steps, for
// a file written a minute ago and for one written long before it is read.
func TestSubagentFileBeforeItsCall(t *testing.T) {
	for _, age := range []time.Duration{time.Minute, 10 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			e := newEnv(t, age)
			l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 6, Start: e.start, Cwd: e.cwd,
				Turns: []sessiongen.Turn{{Say: 20, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true}}}})
			if err != nil {
				t.Fatal(err)
			}
			main, err := os.ReadFile(l.Main())
			if err != nil {
				t.Fatal(err)
			}
			// The conversation as it was before the task call: its first
			// three records (title, header, model).
			if err := os.WriteFile(l.Main(), main[:nthLine(main, 3)], 0o644); err != nil {
				t.Fatal(err)
			}
			r := e.rig(t, "omp")
			if st := r.Settle(5 * time.Second); countKind(st, harness.KindAgent) != 0 {
				t.Fatalf("%d agent rows before any task call", countKind(st, harness.KindAgent))
			}
			if err := os.WriteFile(l.Main(), main, 0o644); err != nil {
				t.Fatal(err)
			}
			st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
				return enginetest.Has(l.Truth.Last)(st) && stepOwners(st) == 2
			})
			if n := countKind(st, harness.KindAgent); n != 2 {
				t.Fatalf("%d agent rows for 2 tasks", n)
			}
			enginetest.Fail(t, "after the call", enginetest.Matched(st, enginetest.Facts{Truth: l.Truth}))
		})
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
