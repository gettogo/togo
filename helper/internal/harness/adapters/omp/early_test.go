package omp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// taskCallLine is a task call at the given time that starts tasks by name.
func taskCallLine(id, at string, names ...string) harness.Record {
	var tasks []string
	for _, n := range names {
		tasks = append(tasks, `{"name":"`+n+`","task":"Complete assignment thoroughly:\n\n# Target\n`+n+`."}`)
	}
	return line("session", `{"type":"message","id":"m-`+id+`","parentId":"e003","timestamp":"`+at+`","message":{"role":"assistant","content":[{"type":"toolCall","id":"`+id+`","name":"task","arguments":{"tasks":[`+strings.Join(tasks, ",")+`]}}],"stopReason":"toolUse"}}`)
}

// bigFile is a subagent file named after task, header at hdrAt, whose
// first message carries pad bytes (a pasted log), read at readAt.
func bigFile(task, sub, hdrAt string, pad int, readAt time.Time) []harness.Record {
	head := `{"type":"session","version":3,"id":"` + sub + `","timestamp":"` + hdrAt + `","cwd":"/home/dev/project","parentSession":"/home/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e.jsonl"}`
	msg := `{"type":"message","id":"` + sub + `-1","parentId":null,"timestamp":"` + hdrAt + `","message":{"role":"user","content":"Audit. ` + strings.Repeat("x", pad) + `"}}`
	var out []harness.Record
	for _, raw := range []string{`{"type":"title","title":""}`, head, msg} {
		out = append(out, harness.Record{Source: "subagents", At: readAt, File: subDir + task + ".jsonl", Data: json.RawMessage(raw)})
	}
	return out
}

// A restart that reads the subagents' folder before the conversation, with
// files that add up to more than any amount the mapper would hold: each
// file still goes to its task, one row per task, recent or old.
func TestManyLargeFilesBeforeTheirCall(t *testing.T) {
	names := []string{"AuditOne", "AuditTwo", "AuditThree", "AuditFour"}
	for name, readAt := range map[string]time.Time{"fresh": t0.Add(2 * time.Minute), "old": t0.Add(time.Hour)} {
		t.Run(name, func(t *testing.T) {
			var files []harness.Record
			for i, n := range names {
				files = append(files, bigFile(n, fmt.Sprintf("sub-%d", i), "2026-11-06T10:01:03.000Z", 3<<20, readAt)...)
			}
			call := taskCallLine("call_big", "2026-11-06T10:01:00.000Z", names...)
			call.At = readAt
			out := run(newSession(env()), opened(t), files, []harness.Record{call})
			want := []string{"agent-AuditFour", "agent-AuditOne", "agent-AuditThree", "agent-AuditTwo"}
			if got := agentRowIDs(out); !slices.Equal(got, want) {
				t.Fatalf("agent rows %v", got)
			}
			for i, n := range names {
				if p := out.last()[fmt.Sprintf("sub-%d-1", i)].Parent; p != "agent-"+n {
					t.Fatalf("%s's step is under %q", n, p)
				}
			}
		})
	}
}

// A task call with no task name is waiting for its file and the
// conversation then stays quiet (the call blocks it). The subagent's file,
// named otherwise, goes to that task at once, and the question it asks
// first is shown without waiting for anything else to be written.
func TestQuietCallTakesItsFileAtOnce(t *testing.T) {
	readAt := time.Date(2026, 11, 6, 10, 1, 4, 0, time.UTC)
	at := func(r harness.Record) harness.Record { r.At = readAt; return r }
	call := at(line("session", `{"type":"message","id":"m-inline","parentId":"e003","timestamp":"2026-11-06T10:01:01.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_inline","name":"task","arguments":{"agent":"explore","task":"Check the parser."}}],"stopReason":"toolUse"}}`))
	head := at(harness.Record{Source: "subagents", File: subDir + "explore-1.jsonl", Data: json.RawMessage(`{"type":"session","version":3,"id":"sub-q","timestamp":"2026-11-06T10:01:02.000Z","cwd":"/home/dev/project","parentSession":"/home/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e.jsonl"}`)})
	ask := at(harness.Record{Source: "subagents", File: subDir + "explore-1.jsonl", Data: json.RawMessage(`{"type":"message","id":"sub-q-1","parentId":null,"timestamp":"2026-11-06T10:01:03.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"toolu_q1","name":"ask","arguments":{"questions":[{"id":"which","question":"Which parser?","options":[{"label":"Old"},{"label":"New"}]}]}}],"stopReason":"toolUse"}}`)})
	out := run(newSession(env()), opened(t), []harness.Record{call, head, ask})
	q := out.last()["ask-toolu_q1"]
	if q.Question == nil || q.Parent != "agent-call_inline-1" {
		t.Fatalf("question %+v under %q", q.Question, q.Parent)
	}
}

// Two calls use one task name; omp's file of that name holds the later
// run. Read before both calls (a restart), or after them, it goes to the
// later call's row, the one started just before the file.
func TestReusedNameGoesToTheCallJustBeforeTheFile(t *testing.T) {
	readAt := t0.Add(time.Hour)
	first, second := taskCallLine("call_1", "2026-11-06T10:01:00.000Z", "explore"), taskCallLine("call_2", "2026-11-06T10:05:00.000Z", "explore")
	first.At, second.At = readAt, readAt
	file := bigFile("explore", "sub-2", "2026-11-06T10:05:02.000Z", 10, readAt)
	for name, order := range map[string][][]harness.Record{
		"file first": {opened(t), file, {first, second}},
		"file last":  {opened(t), {first, second}, file},
	} {
		t.Run(name, func(t *testing.T) {
			out := run(newSession(env()), order...)
			got := out.last()
			if p := got["sub-2-1"].Parent; p == "" || got[p].Agent == nil || got[p].Agent.Started.Format(time.RFC3339) != "2026-11-06T10:05:00Z" {
				t.Fatalf("the later run's step is under %q (%+v)", p, got[p].Agent)
			}
			if n := len(agentRowIDs(out)); n != 2 {
				t.Fatalf("%d agent rows for two calls: %v", n, agentRowIDs(out))
			}
		})
	}
}

// A file of another session in the folder is left out before anything is
// made of it: no row, nothing kept for a call.
func TestOtherSessionsFileMakesNoEarlyRow(t *testing.T) {
	m := newSession(env())
	other := bigFile("Stray", "sub-o", "2026-11-06T10:01:03.000Z", 10, t0)
	other[1].Data = json.RawMessage(strings.Replace(string(other[1].Data), "_0b7c1f2a9e.jsonl", "_ffffffffff.jsonl", 1))
	out := run(m, opened(t), other)
	if got := agentRowIDs(out); len(got) != 0 || len(m.byFile.early) != 0 {
		t.Fatalf("rows %v, early %d", got, len(m.byFile.early))
	}
}

// After a restart only the end of a long conversation is read. A call that
// names its task is waiting for its file when the file of an earlier
// task, whose call is outside what was read, arrives first. That file is
// not the waiting task's: its steps stay out of view,
// and the waiting task's own file still goes to it.
func TestEarlierTasksFileIsNotGivenToANamedWaitingTask(t *testing.T) {
	readAt := t0.Add(2 * time.Hour)
	call := taskCallLine("call_late", "2026-11-06T11:30:00.000Z", "AuditLate")
	call.At = readAt
	earlier := bigFile("AuditEarly", "sub-early", "2026-11-06T10:01:03.000Z", 10, readAt)
	own := bigFile("AuditLate", "sub-late", "2026-11-06T11:30:02.000Z", 10, readAt)
	for name, files := range map[string][]harness.Record{"earlier first": append(slices.Clone(earlier), own...), "own first": append(slices.Clone(own), earlier...)} {
		t.Run(name, func(t *testing.T) {
			out := run(newSession(env()), opened(t), []harness.Record{call}, files)
			// The earlier file is old and no call that was read names it:
			// it stays out of view.
			if it, shown := out.last()["sub-early-1"]; shown {
				t.Errorf("the earlier task's step is shown, under %q", it.Parent)
			}
			if p := out.last()["sub-late-1"].Parent; p != "agent-AuditLate" {
				t.Errorf("the waiting task's own step is under %q", p)
			}
		})
	}
}
