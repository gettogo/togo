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
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func wheelTmux(t *testing.T, c *tmux.Client, args ...string) string {
	t.Helper()
	out, err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTmuxConfBindsTheWheel(t *testing.T) {
	for _, b := range session.MouseBindings("=") {
		line := "bind -n " + b[0] + " " + b[1] + "\n"
		if b[1] == "" {
			line = "unbind -n " + b[0] + "\n"
		}
		if !strings.Contains(session.TmuxConf, line) {
			t.Errorf("tmuxConf lacks the %s binding", b[0])
		}
	}
	if !strings.Contains(session.TmuxConf, "alternate_on") {
		t.Error("tmuxConf does not tell the alternate screen apart")
	}
}

func TestMouseBindingsOnFreshAndRunningServers(t *testing.T) {
	ctx := context.Background()

	// A server started with the config has the bindings.
	fresh := tmuxtest.New(t)
	fresh.Config = filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(fresh.Config, []byte(session.TmuxConf), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fresh.NewSession(ctx, tmux.NewSessionOpts{Name: "f", Dir: t.TempDir()}); err != nil {
		t.Fatalf("tmux rejected the config: %v", err)
	}
	checkBound(t, fresh, "fresh server")

	// A server started by an older helper gets them from KeepServer, and a
	// second run changes nothing.
	s := newService(t)
	if err := s.Tmux.NewSession(ctx, tmux.NewSessionOpts{Name: "old", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := wheelTmux(t, s.Tmux, "list-keys", "-T", "root", "MouseDrag1Pane"); strings.Contains(got, "alternate_on") {
		t.Fatalf("server without the config already has the binding: %q", got)
	}
	for range 2 {
		if err := s.KeepServer(ctx); err != nil {
			t.Fatal(err)
		}
	}
	checkBound(t, s.Tmux, "running server after KeepServer")
}

// checkBound asserts every togo mouse binding is installed, and that no
// right-click binding opens a menu.
func checkBound(t *testing.T, c *tmux.Client, what string) {
	t.Helper()
	keys := wheelTmux(t, c, "list-keys", "-T", "root")
	for _, b := range session.MouseBindings("=") {
		var line string
		for _, l := range strings.Split(keys, "\n") {
			if f := strings.Fields(l); len(f) > 3 && f[3] == b[0] {
				line = l
			}
		}
		switch {
		case b[1] == "" && line != "":
			t.Errorf("%s: %s is still bound: %s", what, b[0], line)
		case b[1] != "" && !strings.Contains(line, "alternate_on") && !strings.Contains(line, "mouse_any_flag"):
			t.Errorf("%s: %s = %q, want the togo binding", what, b[0], line)
		}
		if strings.Contains(line, "display-menu") {
			t.Errorf("%s: %s opens a menu", what, b[0])
		}
	}
}

// runWheel runs the binding's command for key against pane, as tmux runs it
// for a wheel event over that pane.
func runWheel(t *testing.T, c *tmux.Client, key, pane string) {
	t.Helper()
	runBinding(t, c, key, pane)
}

func runBinding(t *testing.T, c *tmux.Client, key, pane string) {
	t.Helper()
	for _, b := range session.MouseBindings(pane) {
		if b[0] == key {
			cmd := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "source-file", "-")...)
			cmd.Stdin = strings.NewReader(b[1] + "\n")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", b[1], err, out)
			}
			return
		}
	}
	t.Fatalf("no binding for %s", key)
}

func TestWheelOnAlternateScreenSendsArrowsNotCopyMode(t *testing.T) {
	ctx := context.Background()
	c := tmuxtest.New(t)
	// The app switches to the alternate screen without asking for the mouse
	// and echoes what it receives.
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name: "alt", Dir: t.TempDir(),
		Command: []string{"sh", "-c", `printf '\033[?1049h'; exec cat -v`},
	}); err != nil {
		t.Fatal(err)
	}
	pane := wheelTmux(t, c, "list-panes", "-t", "=alt:", "-F", "#{pane_id}")
	waitFor(t, func() bool {
		return wheelTmux(t, c, "display", "-p", "-t", pane, "#{alternate_on}") == "1"
	}, "pane on the alternate screen")

	runWheel(t, c, "WheelUpPane", pane)
	runWheel(t, c, "WheelDownPane", pane)
	// A drag, a double click, a triple click and a right click do not enter
	// copy-mode or open a menu either. (A drag on the alternate screen is
	// the same binding as the others: only the mouse-less branch is run.)
	for _, key := range []string{"MouseDrag1Pane", "DoubleClick1Pane", "TripleClick1Pane", "MouseDown3Pane"} {
		runBinding(t, c, key, pane)
	}

	if got := wheelTmux(t, c, "display", "-p", "-t", pane, "#{pane_in_mode}"); got != "0" {
		t.Fatalf("pane_in_mode = %s after a wheel on the alternate screen, want 0", got)
	}
	waitFor(t, func() bool {
		screen := wheelTmux(t, c, "capture-pane", "-p", "-t", pane)
		return strings.Count(screen, "^[[A") >= 3 || strings.Count(screen, "^[OA") >= 3
	}, "three Up arrows reaching the app")
}

func TestWheelUpOnTheNormalScreenEntersCopyMode(t *testing.T) {
	ctx := context.Background()
	c := tmuxtest.New(t)
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name: "norm", Dir: t.TempDir(), Command: []string{"sh", "-c", "exec cat"},
	}); err != nil {
		t.Fatal(err)
	}
	pane := wheelTmux(t, c, "list-panes", "-t", "=norm:", "-F", "#{pane_id}")
	runWheel(t, c, "WheelUpPane", pane)
	if got := wheelTmux(t, c, "display", "-p", "-t", pane, "#{pane_in_mode}"); got != "1" {
		t.Fatalf("pane_in_mode = %s after a wheel up on the normal screen, want 1", got)
	}
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	for range 100 {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// On the normal screen a drag and a double click keep tmux's copy-mode
// selection. They need a real mouse event to run, so the binding text is
// checked.
func TestDragOnTheNormalScreenKeepsCopyMode(t *testing.T) {
	for _, b := range session.MouseBindings("=") {
		switch b[0] {
		case "MouseDrag1Pane":
			if !strings.Contains(b[1], "copy-mode -M") {
				t.Errorf("drag binding lost copy-mode selection: %s", b[1])
			}
		case "DoubleClick1Pane", "TripleClick1Pane":
			if !strings.Contains(b[1], "copy-mode -H") {
				t.Errorf("%s binding lost copy-mode selection: %s", b[0], b[1])
			}
		case "MouseDown3Pane":
			if strings.Contains(b[1], "display-menu") {
				t.Errorf("right click opens a menu: %s", b[1])
			}
		}
	}
}
