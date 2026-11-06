package tmux_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// waitFile returns the contents of path once it exists.
func waitFile(t *testing.T, path string) string {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never written", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRespawnPaneKeepsSessionAndEnvironment replaces a running command
// with another one, in a new folder, in the same session: the session's
// environment carries over and argv reaches the program literally.
func TestRespawnPaneKeepsSessionAndEnvironment(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()
	err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_abcdefghij", Dir: first, Command: []string{"/bin/sleep", "60"},
		Env: map[string]string{"TOGO_SESSION": "s_abcdefghij"}})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(second, "out")
	script := `printf '%s|%s|%s' "$TOGO_SESSION" "$(pwd -P)" "$1" > out.tmp && mv out.tmp out; exec /bin/sleep 60`
	if err := c.RespawnPane(ctx, "=s_abcdefghij:", second, []string{"/bin/sh", "-c", script, "sh", "a;b $(x) #{session_name};"}); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(second)
	if got, want := waitFile(t, out), "s_abcdefghij|"+real+"|a;b $(x) #{session_name};"; got != want {
		t.Fatalf("respawned pane wrote %q, want %q", got, want)
	}
	infos, err := c.ListSessions(ctx)
	if err != nil || len(infos) != 1 || infos[0].Name != "s_abcdefghij" {
		t.Fatalf("sessions after respawn %+v %v", infos, err)
	}
}

func TestRespawnPaneMissingSession(t *testing.T) {
	c := tmuxtest.New(t)
	err := c.RespawnPane(context.Background(), "=s_abcdefghij:", t.TempDir(), []string{"/bin/true"})
	if !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("err %v, want ErrNoSession", err)
	}
}

// TestForegroundNamesTheRunningJob: while a job runs under an interactive
// shell it is the pane's foreground; once the shell is back at its prompt
// the pane's own process is.
func TestForegroundNamesTheRunningJob(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	dir := t.TempDir()
	flag := filepath.Join(dir, "go")
	script := `while [ ! -e "$1" ]; do /bin/sleep 0.05; done; exec /bin/sh -i`
	err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_abcdefghij", Dir: dir, Command: []string{"/bin/sh", "-c", script, "sh", flag},
		Env: map[string]string{"ENV": "", "PS1": "$ "}})
	if err != nil {
		t.Fatal(err)
	}
	var fg tmux.PaneForeground
	for deadline := time.Now().Add(10 * time.Second); ; {
		if fg, err = c.Foreground(ctx, "s_abcdefghij"); err == nil && hasName(fg, "sleep") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep never in the foreground: %+v %v", fg, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if fg.Dead || fg.PanePID < 1 {
		t.Fatalf("foreground %+v", fg)
	}
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		fg, err = c.Foreground(ctx, "s_abcdefghij")
		if err == nil && len(fg.Foreground) == 1 && fg.Foreground[0].PID == fg.PanePID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell never back in the foreground: %+v %v", fg, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := c.Foreground(ctx, "s_bbbbbbbbbb"); err == nil {
		t.Fatal("no error for a missing session")
	}
}

func hasName(fg tmux.PaneForeground, name string) bool {
	for _, p := range fg.Foreground {
		if strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}
