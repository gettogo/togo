package omp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

const subDir = "/home/dev/.omp/agent/sessions/-project/2026-11-06T10-00-00-000Z_0b7c1f2a9e/"

// fileRecs are a subagent file's header and first message, read from the
// file named after task, the way the file source delivers them.
func fileRecs(task, sub string) []harness.Record {
	var out []harness.Record
	for _, raw := range []string{bgHead, bgSubMsg} {
		out = append(out, harness.Record{Source: "subagents", At: t0, File: subDir + task + ".jsonl",
			Data: json.RawMessage(strings.ReplaceAll(raw, "SUBID", sub))})
	}
	return out
}

func launched(t *testing.T) []harness.Record {
	t.Helper()
	recs := records(t, "session.jsonl", "session")
	for _, l := range []string{bgCall, bgResult, bgSaid} {
		recs = append(recs, line("session", l))
	}
	return recs
}

// Each file goes to the task it is named after, whatever order the files
// are read in.
func TestSubagentFileGoesToTheTaskItIsNamedAfter(t *testing.T) {
	m := newSession(env())
	got := run(m, launched(t), fileRecs("AuditTwo", "sub-two"), fileRecs("AuditOne", "sub-one")).last()
	if p := got["sub-two-1"].Parent; p != "agent-AuditTwo" {
		t.Fatalf("AuditTwo's step is under %q", p)
	}
	if p := got["sub-one-1"].Parent; p != "agent-AuditOne" {
		t.Fatalf("AuditOne's step is under %q", p)
	}
	if len(m.pending) != 0 {
		t.Fatalf("tasks still waiting for a file: %v", m.pending)
	}
}

// An entry with no known parent belongs to its own file's agent, not to
// the file whose header was read last.
func TestInterleavedFilesKeepTheirAgents(t *testing.T) {
	one, two := fileRecs("AuditOne", "sub-one"), fileRecs("AuditTwo", "sub-two")
	got := run(newSession(env()), launched(t), []harness.Record{one[0], two[0], one[1], two[1]}).last()
	if p := got["sub-one-1"].Parent; p != "agent-AuditOne" {
		t.Fatalf("AuditOne's step is under %q", p)
	}
	if p := got["sub-two-1"].Parent; p != "agent-AuditTwo" {
		t.Fatalf("AuditTwo's step is under %q", p)
	}
}

// A file whose name no task has (an omp that named files otherwise) still
// takes the first task waiting for a file.
func TestUnnamedFileFallsBackToTheFirstWaitingTask(t *testing.T) {
	got := run(newSession(env()), launched(t), fileRecs("0-Other", "sub-x")).last()
	if p := got["sub-x-1"].Parent; p != "agent-AuditOne" {
		t.Fatalf("the step is under %q", p)
	}
}
