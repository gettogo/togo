package procs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDescendantsFollowsChildren(t *testing.T) {
	table := ParsePS("  1 0\n 10 1\n 11 10\n 12 11\n 20 1\nbad line\n")
	got := Descendants(table, []int{10})
	slices.Sort(got)
	if !slices.Equal(got, []int{10, 11, 12}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseLsof(t *testing.T) {
	real := map[string]string{"/r/a.jsonl": "/a.jsonl", "/r/b.jsonl": "/b.jsonl"}
	out := []byte("p42\nn/r/a.jsonl\np43\nn/other\nn/r/a.jsonl\np44\nn/r/b.jsonl\n")
	got := ParseLsof(out, real)
	if !slices.Equal(got["/a.jsonl"], []int{42, 43}) || !slices.Equal(got["/b.jsonl"], []int{44}) {
		t.Fatalf("got %v", got)
	}
}

// This test process holds a file open: Holders must name it, and name
// nobody for a file nothing holds.
func TestHoldersFindsThisProcess(t *testing.T) {
	dir := t.TempDir()
	held, idle := filepath.Join(dir, "held.jsonl"), filepath.Join(dir, "idle.jsonl")
	f, err := os.OpenFile(held, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.WriteFile(idle, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Holders(context.Background(), []string{held, idle})
	if err != nil {
		t.Skipf("no way to list open files here: %v", err)
	}
	if !slices.Contains(got[held], os.Getpid()) {
		t.Fatalf("held: %v", got)
	}
	if len(got[idle]) != 0 {
		t.Fatalf("idle: %v", got)
	}
}
