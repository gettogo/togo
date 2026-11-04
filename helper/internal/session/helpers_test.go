package session_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

var fixedNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// newService returns a Service on a private tmux label with its own state
// directory. SHELL is pinned to /bin/sh so sessions start fast and never
// read the owner's shell startup files.
func newService(t *testing.T) *session.Service {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	return &session.Service{
		Tmux:     tmuxtest.New(t),
		Recent:   &state.Recent{File: filepath.Join(dir, "recent.json")},
		LockFile: filepath.Join(dir, "run", "bib.lock"),
		Shell:    "/bin/sh",
		Now:      func() time.Time { return fixedNow },
	}
}

func mustCreate(t *testing.T, s *session.Service, o session.CreateOpts) session.Session {
	t.Helper()
	if o.Cwd == "" {
		o.Cwd = t.TempDir()
	}
	got, err := s.Create(context.Background(), o)
	if err != nil {
		t.Fatalf("Create(%+v): %v", o, err)
	}
	return got
}

func mustList(t *testing.T, s *session.Service) []session.Session {
	t.Helper()
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return r
}

// tmuxOut runs a read-only tmux command on c's socket and returns its
// trimmed output.
func tmuxOut(t *testing.T, c *tmux.Client, args ...string) string {
	t.Helper()
	out, err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), args...)...).Output()
	if err != nil {
		t.Fatalf("tmux %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
