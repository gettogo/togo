package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// runCounter writes a stand-in agent that appends "<TOGO_SESSION> <args>"
// to runs in its folder each time it runs, and exits 1.
func runCounter(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "agent")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$TOGO_SESSION\" \"$*\" >> \"$(dirname \"$0\")/runs\"\nexit 1\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitRuns(t *testing.T, dir string, n int) []string {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		b, _ := os.ReadFile(filepath.Join(dir, "runs"))
		if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(b) > 0 && len(lines) >= n {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent ran fewer than %d times: %q", n, b)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitAgent waits until the session's pane shows its agent gone, or
// running when gone is false.
func waitAgent(t *testing.T, s *session.Service, id string, gone bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if g, err := s.AgentGone(context.Background(), id); err == nil && g == gone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pane never showed the agent gone=%v", gone)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRelaunchRespawnsSameIDAndBib(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	created := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: runCounter(t, dir), Args: []string{"--flag", "a b;"}, Cwd: dir})
	waitRuns(t, dir, 1)
	waitAgent(t, s, created.ID, true)
	got, err := s.Relaunch(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	runs := waitRuns(t, dir, 2)
	want := created.ID + " --flag a b;"
	if len(runs) != 2 || runs[0] != want || runs[1] != want {
		t.Fatalf("runs %q, want two of %q", runs, want)
	}
	if got.ID != created.ID || *got.Bib != *created.Bib || got.Name != created.Name || got.Cwd != dir ||
		strings.Join(got.Command, "|") != strings.Join(created.Command, "|") {
		t.Fatalf("relaunched %+v, created %+v", got, created)
	}
	if n := len(mustList(t, s)); n != 1 {
		t.Fatalf("%d sessions after relaunch", n)
	}
}

// A live agent is never killed, whatever status the session reads: a
// usage limit leaves Claude running at its prompt with the session failed.
func TestRelaunchRefusesRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	s.Path = fakeBin(t, "claude")
	created := mustCreate(t, s, session.CreateOpts{Agent: "claude"})
	waitAgent(t, s, created.ID, false)
	if _, err := s.Relaunch(context.Background(), created.ID); !errors.Is(err, session.ErrRunning) {
		t.Fatalf("err %v, want ErrRunning", err)
	}
}

func TestRelaunchUnmanaged(t *testing.T) {
	s := newService(t)
	if _, err := s.Relaunch(context.Background(), "u_6d79"); !errors.Is(err, session.ErrNotManaged) {
		t.Fatalf("err %v, want ErrNotManaged", err)
	}
	if _, err := s.Relaunch(context.Background(), "s_bbbbbbbbbb"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("err %v, want ErrNotFound", err)
	}
}

// TestRelaunchChecksProgramAgain: a program removed since the session
// started is not installed, and nothing runs.
func TestRelaunchChecksProgramAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	prog := runCounter(t, dir)
	created := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: prog, Cwd: dir})
	waitRuns(t, dir, 1)
	waitAgent(t, s, created.ID, true)
	if err := os.Rename(prog, prog+".gone"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Relaunch(context.Background(), created.ID)
	if reason(t, err) != wire.ReasonNotInstalled {
		t.Fatalf("reason %q, want not_installed", reason(t, err))
	}
}

// TestRelaunchShellSession: a plain shell has nothing to run again.
func TestRelaunchShellSession(t *testing.T) {
	s := newService(t)
	created := mustCreate(t, s, session.CreateOpts{})
	waitAgent(t, s, created.ID, true)
	var ipe *session.InvalidParamsError
	if _, err := s.Relaunch(context.Background(), created.ID); !errors.As(err, &ipe) {
		t.Fatalf("err %v, want invalid params", err)
	}
}
