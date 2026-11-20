package procs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The test binary is the one process of its name in a tree rooted at
// itself; a tree that cannot be listed is unknown, not zero.
func TestWithinCountsTheTreesOwnProcesses(t *testing.T) {
	s := Session{Panes: func(context.Context) ([]int, error) { return []int{os.Getpid()}, nil }}
	n, err := s.Within(context.Background(), []string{filepath.Base(os.Args[0])})
	if err != nil || n != 1 {
		t.Fatalf("Within = %d, %v; want 1", n, err)
	}
	if n, err := s.Within(context.Background(), []string{"no-such-program"}); err != nil || n != 0 {
		t.Fatalf("Within of another program = %d, %v", n, err)
	}
	if _, err := (Session{}).Within(context.Background(), []string{"x"}); err == nil {
		t.Fatal("an unknown tree gave an answer")
	}
}
