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

// A file read before the call that starts its task gets the row the call
// then adopts: one row per task, whichever feed is read first, and also for
// a file read long after it was written (after a restart).
func TestFileReadBeforeItsCallIsAdoptedByIt(t *testing.T) {
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
			if a := got["agent-AuditOne"]; a.Agent.Started.Format(time.RFC3339) != "2026-11-06T10:01:00Z" { // the call's time, not the file's
				t.Fatalf("adopted row %+v", a.Agent)
			}
		})
	}
}

// A file no call names: a recent one shows its row at once, under its task
// name, with nothing else needing to be read; an old one (an earlier
// turn's, whose call was not read) is left out, also once the conversation
// is read past it.
func TestFileNoCallNamesIsPlacedAtOnce(t *testing.T) {
	fresh := run(newSession(env()), opened(t), fileRecs("Stray", "sub-x"))
	if got := agentRowIDs(fresh); !slices.Equal(got, []string{"agent-Stray"}) || fresh.last()["sub-x-1"].Parent != "agent-Stray" {
		t.Fatalf("fresh: rows %v, step under %q", got, fresh.last()["sub-x-1"].Parent)
	}
	m := newSession(env())
	later := line("session", bgDone) // written after the file started
	old := run(m, opened(t), readLater(fileRecs("Stray", "sub-x"), time.Hour), readLater([]harness.Record{later}, time.Hour))
	if got := agentRowIDs(old); len(got) != 0 {
		t.Fatalf("old: rows %v", got)
	}
	if ef := m.byFile.early["agent-Stray"]; ef == nil || !ef.gone || ef.items != nil {
		t.Fatalf("old file's items still kept: %+v", ef)
	}
}
