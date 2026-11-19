package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The faults below share one cause. Restoring a file over 2,000 records
// or 8 MiB, the tail keeps the file's first line as its header
// (source/file.go:354 and :398). An omp file opens with a title record;
// the session header is its second line, so it is cut. The mapper then
// never learns the conversation's id (adapters/omp/session.go header):
//   - the engine takes the next id it sees as the one the session started
//     on (engine/conversation.go:71), so a /new after the restore does not
//     start the chat over, and the old conversation stays in it;
//   - with no terminal record, the subagents feed needs that id
//     (adapters/omp/session.go:98), so no subagent's steps are shown.
//
// Correct: the restore keeps the record that names the session (for omp,
// the first "session" record), or the mapper reads the header apart from
// the restore window. These tests pass while the faults are present.

// TestKnownFault_HeaderLostAboveRestoreLimit: /new after restoring a file
// past either limit leaves the old conversation in the chat. Below and at
// the limits the chat starts over (TestOMPRecordLimit, TestOMPSizeLimit).
func TestKnownFault_HeaderLostAboveRestoreLimit(t *testing.T) {
	t.Run("records", func(t *testing.T) {
		e := newEnv(t, time.Hour)
		l, err := sessiongen.Write(e.home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 41, Start: e.start, Cwd: e.cwd, Turns: byRecords(maxRecords + 1)})
		if err != nil {
			t.Fatal(err)
		}
		if restoreThenNew(t, e, l) {
			t.Fatal("the chat started over after /new: the fault is fixed")
		}
	})
	t.Run("size", func(t *testing.T) {
		e := newEnv(t, time.Hour)
		l := bySize(t, e, maxFileRead+1024, sessiongen.Script{Seed: 42, Start: e.start, Cwd: e.cwd})
		if restoreThenNew(t, e, l) {
			t.Fatal("the chat started over after /new: the fault is fixed")
		}
	})
}

// TestKnownFault_NoSubagentsAfterLongRestoreWithoutTerminalRecord: with
// no terminal record, a file at the record limit shows its subagents'
// steps and one record past it shows none.
func TestKnownFault_NoSubagentsAfterLongRestoreWithoutTerminalRecord(t *testing.T) {
	owners := func(n int) int {
		e := newEnv(t, time.Hour)
		turns := byRecords(n - 6) // the batch adds six records
		turns[len(turns)-1].Agents = &sessiongen.Batch{Size: 2, Steps: 1, Background: true}
		l, err := sessiongen.Write(e.home, &sessiongen.OMP{}, sessiongen.Script{Seed: 44, Start: e.start, Cwd: e.cwd, Turns: turns})
		if err != nil {
			t.Fatal(err)
		}
		if got := l.Records[l.Main()]; got != n {
			t.Fatalf("main file holds %d records, want %d", got, n)
		}
		r := e.rigWith(t, enginetest.Options{Agent: "omp", NoRedact: true, MaxItems: 20000})
		st := r.SettleUntil(20*time.Second, enginetest.Has(l.Truth.Last))
		return stepOwners(st)
	}
	if n := owners(maxRecords); n != 2 {
		t.Fatalf("at the limit %d of 2 agents show steps, want 2", n)
	}
	if n := owners(maxRecords + 1); n != 0 {
		t.Fatalf("past the limit %d of 2 agents show steps: the fault is fixed", n)
	}
}
