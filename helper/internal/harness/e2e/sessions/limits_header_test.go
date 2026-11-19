package sessions

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Restoring a file over 2,000 records or 8 MiB keeps only its last
// records and the record that names the session. An omp file opens with a
// title record and names its session on the second line; the restore
// keeps both (the feed's header: session), so the mapper learns the
// conversation's id:
//   - a /new after the restore starts the chat over;
//   - with no terminal record, the subagents feed, which needs that id,
//     shows the subagents' steps.

// TestHeaderKeptAboveRestoreLimit: /new after restoring a file past
// either limit starts the chat over, as below and at the limits
// (TestOMPRecordLimit, TestOMPSizeLimit).
func TestHeaderKeptAboveRestoreLimit(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	t.Run("records", func(t *testing.T) {
		e := support.NewEnv(t, time.Hour)
		l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 41, Start: e.Start, Cwd: e.Cwd, Turns: byRecords(maxRecords + 1)})
		if err != nil {
			t.Fatal(err)
		}
		if !restoreThenNew(t, e, l) {
			t.Error("after /new the chat did not start over")
		}
	})
	t.Run("size", func(t *testing.T) {
		e := support.NewEnv(t, time.Hour)
		l := bySize(t, e, maxFileRead+1024, sessiongen.Script{Seed: 42, Start: e.Start, Cwd: e.Cwd})
		if !restoreThenNew(t, e, l) {
			t.Error("after /new the chat did not start over")
		}
	})
}

// TestSubagentsAfterLongRestoreWithoutTerminalRecord: with no terminal
// record, a file at the record limit and one record past it both show
// their subagents' steps.
func TestSubagentsAfterLongRestoreWithoutTerminalRecord(t *testing.T) {
	t.Parallel()
	owners := func(n int) int {
		e := support.NewEnv(t, time.Hour)
		turns := byRecords(n - 6) // the batch adds six records
		turns[len(turns)-1].Agents = &sessiongen.Batch{Size: 2, Steps: 1, Background: true}
		l, err := sessiongen.Write(e.Home, &sessiongen.OMP{}, sessiongen.Script{Seed: 44, Start: e.Start, Cwd: e.Cwd, Turns: turns})
		if err != nil {
			t.Fatal(err)
		}
		if got := l.Records[l.Main()]; got != n {
			t.Fatalf("main file holds %d records, want %d", got, n)
		}
		r := e.RigWith(t, enginetest.Options{Agent: "omp", NoRedact: true, MaxItems: 20000})
		st := r.SettleUntil(20*time.Second, func(st enginetest.State) bool {
			return enginetest.Has(l.Truth.Last)(st) && stepOwners(st) == 2
		})
		return stepOwners(st)
	}
	for _, n := range []int{maxRecords, maxRecords + 1} {
		if got := owners(n); got != 2 {
			t.Fatalf("%d records: %d of 2 agents show steps", n, got)
		}
	}
}
