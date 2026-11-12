package procs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
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

func TestMatchCommands(t *testing.T) {
	out := "  10 /usr/bin/zsh -lic togo omp\n  11 /opt/homebrew/bin/omp --resume\n  12 node /usr/lib/bin/pi -c\n  13 node /usr/lib/bin/other\n  14 vim pi.txt\n"
	got := MatchCommands(out, []string{"omp", "pi"})
	if !slices.Equal(got, []int{11, 12}) {
		t.Fatalf("got %v", got)
	}
}

// A process of the named command that this process started in a folder is
// found, unless it is in the session's tree.
func TestOthersFindsAgentInFolderOutsideTree(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(200 * time.Millisecond)
	ctx := context.Background()
	pane := exec.Command("sleep", "30") // a pane process unrelated to the agent
	if err := pane.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pane.Process.Kill(); _ = pane.Wait() }()
	inTree := Session{Panes: func(context.Context) ([]int, error) { return []int{pane.Process.Pid}, nil }}
	got, err := inTree.Others(ctx, []string{"fakeagent"}, dir)
	if err != nil || !got {
		t.Fatalf("outside the tree: %v %v", got, err)
	}
	if got, _ := inTree.Others(ctx, []string{"fakeagent"}, t.TempDir()); got {
		t.Fatal("matched another folder")
	}
	withChild := Session{Panes: func(context.Context) ([]int, error) { return []int{pane.Process.Pid, cmd.Process.Pid}, nil }}
	if got, _ := withChild.Others(ctx, []string{"fakeagent"}, dir); got {
		t.Fatal("counted a process in the tree")
	}
}
