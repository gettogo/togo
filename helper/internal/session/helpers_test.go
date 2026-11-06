package session_test

import (
	"context"
	"os"
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
// read the owner's shell startup files. Agents run under testShell with
// PATH reduced to /usr/bin and /bin, so a pane only ever finds a program a
// test put on PATH with fakeBin, never an agent installed on this machine.
func newService(t *testing.T) *session.Service {
	t.Helper()
	tm := tmuxtest.New(t)
	if bin, err := exec.LookPath(tm.Bin); err == nil {
		tm.Bin = bin
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", "/usr/bin:/bin")
	dir := t.TempDir()
	return &session.Service{
		Tmux:     tm,
		Recent:   &state.Recent{File: filepath.Join(dir, "recent.json")},
		LockFile: filepath.Join(dir, "run", "bib.lock"),
		Manifest: &session.Manifest{Path: filepath.Join(dir, "sessions.json")},
		Shell:    testShell(t),
		Now:      func() time.Time { return fixedNow },
	}
}

// testShell returns a stand-in login shell named sh. It runs the -c script
// with its arguments like sh -lic would, but reads no startup file: a
// login shell's profile (path_helper on macOS) would put system
// directories such as /usr/local/bin ahead of the test's PATH.
func testShell(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sh")
	script := "#!/bin/sh\n[ \"$1\" = -lic ] || exit 64\nshift\nexec /bin/sh -c \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
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
