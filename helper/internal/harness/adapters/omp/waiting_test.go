package omp

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// opened is the main conversation before any task call: its title, header
// and model.
func opened(t *testing.T) []harness.Record {
	t.Helper()
	return records(t, "session.jsonl", "session")[:3]
}

func agentRowIDs(m mapped) []string {
	var out []string
	for id, it := range m.last() {
		if it.Kind == harness.KindAgent {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// readLater moves records to a later read time.
func readLater(recs []harness.Record, d time.Duration) []harness.Record {
	out := append([]harness.Record(nil), recs...)
	for i := range out {
		out[i].At = out[i].At.Add(d)
	}
	return out
}

// A file read before the call that starts its task waits for the call and
// then goes to its task: one row per task, whichever feed is read first,
// and also for a file read long after it was written (after a restart).
func TestFileReadBeforeItsCallWaitsForIt(t *testing.T) {
	for name, late := range map[string]time.Duration{"fresh": 0, "old": time.Hour} {
		t.Run(name, func(t *testing.T) {
			var call []harness.Record
			for _, l := range []string{bgCall, bgResult, bgSaid} {
				call = append(call, line("session", l))
			}
			out := run(newSession(env()), opened(t), readLater(fileRecs("AuditTwo", "sub-two"), late),
				readLater(fileRecs("AuditOne", "sub-one"), late), readLater(call, late))
			if got := agentRowIDs(out); !slices.Equal(got, []string{"agent-AuditOne", "agent-AuditTwo"}) {
				t.Fatalf("agent rows %v", got)
			}
			got := out.last()
			if got["sub-one-1"].Parent != "agent-AuditOne" || got["sub-two-1"].Parent != "agent-AuditTwo" {
				t.Fatalf("steps under %q and %q", got["sub-one-1"].Parent, got["sub-two-1"].Parent)
			}
		})
	}
}

// A file no call names, once the conversation was read past its start,
// is placed as before: a fresh one gets a row of its own, an old one (an
// earlier turn's, whose call was not read) is left out.
func TestFileNoCallNamesIsPlacedWhenTheConversationPassesIt(t *testing.T) {
	later := line("session", bgDone) // written after the file started
	fresh := run(newSession(env()), opened(t), fileRecs("Stray", "sub-x"), []harness.Record{later})
	if got := agentRowIDs(fresh); !slices.Equal(got, []string{"agent-sub-x"}) || fresh.last()["sub-x-1"].Parent != "agent-sub-x" {
		t.Fatalf("fresh: rows %v, step under %q", got, fresh.last()["sub-x-1"].Parent)
	}
	old := run(newSession(env()), opened(t), readLater(fileRecs("Stray", "sub-x"), time.Hour), readLater([]harness.Record{later}, time.Hour))
	if got := agentRowIDs(old); len(got) != 0 {
		t.Fatalf("old: rows %v", got)
	}
}

// With nothing more read from the conversation, a waiting file is placed
// after agentWait, on the next record read.
func TestWaitingFileIsPlacedAfterTheWait(t *testing.T) {
	m := newSession(env())
	recs := fileRecs("Stray", "sub-x")
	if out := run(m, opened(t), recs[:1]); len(agentRowIDs(out)) != 0 {
		t.Fatalf("placed before the wait: %v", agentRowIDs(out))
	}
	out := run(m, readLater(recs[1:], agentWait+time.Second))
	if got := agentRowIDs(out); !slices.Equal(got, []string{"agent-sub-x"}) {
		t.Fatalf("rows %v", got)
	}
	if len(m.byFile.waiting) != 0 || m.byFile.held != 0 {
		t.Fatalf("still held: %d files, %d bytes", len(m.byFile.waiting), m.byFile.held)
	}
}
