package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The restore limits (source/source.go: MaxFileRead 8 MiB, MaxRecords
// 2,000, MaxGlobFiles 200), each one below, at and above.

const (
	maxFileRead  = 8 << 20
	maxRecords   = 2000
	maxGlobFiles = 200
)

// byRecords is a script whose main file holds exactly n records: three
// opening records, turns of ten, and a last turn that takes the rest.
func byRecords(n int) []sessiongen.Turn {
	var turns []sessiongen.Turn
	rem := n - 3
	for rem >= 12 {
		turns = append(turns, sessiongen.Turn{Say: 40, Tools: 4, Output: 100})
		rem -= 10
	}
	last := sessiongen.Turn{Say: 40}
	if rem%2 == 1 {
		last.Compact = true
		rem--
	}
	last.Tools, last.Output = (rem-2)/2, 100
	return append(turns, last)
}

// bySize writes a script whose main file is exactly size bytes: turns of
// large tool output, the last one's output sized to fit. Text is exact in
// length and needs no escaping, so one trial write tells the size.
func bySize(t *testing.T, e support.Env, size int, s sessiongen.Script) *sessiongen.Layout {
	t.Helper()
	turns := func(last int) []sessiongen.Turn {
		var out []sessiongen.Turn
		for i := 0; i < size/(4*60_000); i++ {
			out = append(out, sessiongen.Turn{Say: 40, Tools: 4, Output: 60_000})
		}
		return append(out, sessiongen.Turn{Say: 40, Tools: 1, Output: last})
	}
	s.Turns = turns(1000)
	l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042"}, s)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(l.Main())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.Home, ".omp")); err != nil {
		t.Fatal(err)
	}
	s.Turns = turns(1000 + size - int(fi.Size()))
	l, err = sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042"}, s)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(l.Main()); fi.Size() != int64(size) {
		t.Fatalf("main file is %d bytes, want %d", fi.Size(), size)
	}
	return l
}

// restoreThenNew restores the session, checks every rule, then has omp
// start a new conversation, and reports whether the chat started over
// (a new epoch, fewer items): that needs the restore to have kept the
// file's session header.
func restoreThenNew(t *testing.T, e support.Env, l *sessiongen.Layout) bool {
	t.Helper()
	r := e.RigWith(t, enginetest.Options{Agent: "omp", NoRedact: true, MaxItems: 20000})
	st := r.SettleUntil(20*time.Second, enginetest.Has(l.Truth.Last))
	support.Check(t, "restored", st, l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 30}); err != nil {
		t.Fatal(err)
	}
	after := r.SettleUntil(20*time.Second, enginetest.Has(l.Truth.Last))
	return after.Epoch != st.Epoch && len(after.Items) < len(st.Items)
}

func TestOMPRecordLimit(t *testing.T) {
	coverage.Covers(t, "generated", "omp/scale/at_limit")
	for _, c := range []struct {
		name string
		n    int
	}{{"below", maxRecords - 1}, {"at", maxRecords}} {
		t.Run(c.name, func(t *testing.T) {
			e := support.NewEnv(t, time.Hour)
			l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 41, Start: e.Start, Cwd: e.Cwd, Turns: byRecords(c.n)})
			if err != nil {
				t.Fatal(err)
			}
			if got := l.Records[l.Main()]; got != c.n {
				t.Fatalf("main file holds %d records, want %d", got, c.n)
			}
			if !restoreThenNew(t, e, l) {
				t.Error("after /new the chat did not start over")
			}
		})
	}
}

func TestOMPSizeLimit(t *testing.T) {
	coverage.Covers(t, "generated", "omp/scale/at_limit")
	for _, c := range []struct {
		name string
		size int
	}{{"below", maxFileRead - 1024}, {"at", maxFileRead}} {
		t.Run(c.name, func(t *testing.T) {
			e := support.NewEnv(t, time.Hour)
			l := bySize(t, e, c.size, sessiongen.Script{Seed: 42, Start: e.Start, Cwd: e.Cwd})
			if !restoreThenNew(t, e, l) {
				t.Error("after /new the chat did not start over")
			}
		})
	}
}

// One background batch of n subagents, each file with one step.
func TestOMPSubagentFolderLimit(t *testing.T) {
	coverage.Covers(t, "generated", "omp/scale/at_limit")
	for _, c := range []struct {
		name string
		n    int
	}{{"below", maxGlobFiles - 1}, {"at", maxGlobFiles}, {"above", maxGlobFiles + 1}} {
		t.Run(c.name, func(t *testing.T) {
			e := support.NewEnv(t, time.Hour)
			l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042"}, sessiongen.Script{Seed: 43, Start: e.Start, Cwd: e.Cwd,
				Turns: []sessiongen.Turn{{Say: 30, Agents: &sessiongen.Batch{Size: c.n, Steps: 1, Background: true}}}})
			if err != nil {
				t.Fatal(err)
			}
			r := e.RigWith(t, enginetest.Options{Agent: "omp", NoRedact: true, MaxItems: 20000})
			st := r.SettleUntil(20*time.Second, func(st enginetest.State) bool {
				return enginetest.Has(l.Truth.Last)(st) && stepOwners(st) >= min(c.n, maxGlobFiles)
			})
			support.Check(t, "restored", st, l)
			if n := support.CountKind(st, harness.KindAgent); n != c.n {
				t.Errorf("%d agent items, want %d", n, c.n)
			}
			// Past the limit the oldest file is not read again: its agent
			// keeps its row, without steps.
			if n := stepOwners(st); n != min(c.n, maxGlobFiles) {
				t.Errorf("%d of %d agents show their steps, want %d", n, c.n, min(c.n, maxGlobFiles))
			}
		})
	}
}

// stepOwners counts the agents whose own steps are shown.
func stepOwners(st enginetest.State) int {
	owners := map[string]bool{}
	for _, it := range st.Items {
		if it.Parent != "" && it.User != nil && strings.HasPrefix(it.User.Text, "Complete assignment") {
			owners[it.Parent] = true
		}
	}
	return len(owners)
}
