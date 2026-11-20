package sessiongen

import (
	"path/filepath"
	"testing"
	"time"
)

// What the OMP profile's extras write (omp_extra.go) has the fields of the
// real records: model and effort changes, a command run with "!", edits,
// a search, a picture read, thinking, the todo tool, an answered, a
// cancelled and an open ask, and a fork.
func TestOMPExtrasMatchRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	extra := map[int]OMPTurn{
		1: {Model: "moonshot/kimi-k3", Effort: "high", Bash: "go test ./...", Think: true, Edits: 1, Search: 1, Picture: "logo.png",
			Todo: []OMPTodo{{Op: "init", Phases: []OMPPhase{{Name: "Fix", Tasks: []string{"Read", "Edit"}}}}, {Op: "write", Tasks: []string{"Read", "Edit"}, Done: 1}}},
		2: {Ask: &OMPAsk{ID: "pick", Header: "Pick", Question: "Which?", Options: []string{"A", "B"}, Answer: "B"}},
		3: {Ask: &OMPAsk{ID: "skip", Header: "Skip", Question: "Again?", Options: []string{"Yes", "No"}, Cancelled: true}},
		4: {Ask: &OMPAsk{ID: "open", Header: "Open", Question: "Still?", Options: []string{"Yes"}}},
		5: {Fork: true},
	}
	l, err := Write(home, &OMP{TTY: "ttys042", Extra: extra}, Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd,
		Turns: []Turn{{Say: 20}, {Say: 20}, {Say: 20}, {Say: 20}, {Say: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d conversation files, want the fork's too", len(l.Mains))
	}
	real, _ := filepath.Glob("../adapters/omp/testdata/*.jsonl")
	checkHonest(t, readRecords(t, real), readRecords(t, l.Mains), ompKind, readOMP)
}
