package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tap"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// brewLayout builds <prefix>/Cellar/togo/1.0.0/bin/togo and the
// <prefix>/bin/togo symlink Homebrew points at it, and returns both.
func brewLayout(t *testing.T) (cellar, stable string) {
	t.Helper()
	prefix, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cellar = filepath.Join(prefix, "Cellar", "togo", "1.0.0", "bin", "togo")
	stable = filepath.Join(prefix, "bin", "togo")
	for _, dir := range []string{filepath.Dir(cellar), filepath.Dir(stable)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(cellar, []byte("togo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/togo/1.0.0/bin/togo", stable); err != nil {
		t.Fatal(err)
	}
	return cellar, stable
}

func TestStableExecutablePrefersBrewPrefix(t *testing.T) {
	cellar, stable := brewLayout(t)
	if got := StableExecutable(cellar); got != stable {
		t.Errorf("StableExecutable(%s) = %s, want %s", cellar, got, stable)
	}
	if !InCellar(cellar) || InCellar(stable) {
		t.Errorf("InCellar(cellar)=%v InCellar(stable)=%v, want true and false", InCellar(cellar), InCellar(stable))
	}

	// After brew links another version the symlink no longer names this
	// binary, so the path that runs is kept.
	other := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(cellar))), "1.0.1", "bin", "togo")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("togo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stable); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, stable); err != nil {
		t.Fatal(err)
	}
	if got := StableExecutable(cellar); got != cellar {
		t.Errorf("with the link moved, StableExecutable = %s, want %s", got, cellar)
	}
}

func TestStableExecutableKeepsPlainPath(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "bin", "togo")
	for _, exe := range []string{
		plain,
		"/usr/local/bin/togo",
		filepath.Join(dir, "Cellar", "togo", "1.0.0", "bin", "togo"), // no prefix/bin/togo link
		filepath.Join(dir, "Cellar", "other", "1.0.0", "bin", "togo"),
		"Cellar/togo/1.0.0/bin/togo",
	} {
		if got := StableExecutable(exe); got != exe {
			t.Errorf("StableExecutable(%s) = %s, want it unchanged", exe, got)
		}
	}
}

func TestServiceFilesUseStablePath(t *testing.T) {
	cellar, stable := brewLayout(t)
	for _, m := range []Manager{Launchd, Systemd} {
		s, _ := newTestService(t, m)
		s.Exe = cellar
		if err := s.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(s.Path())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "Cellar") || !strings.Contains(string(data), stable) {
			t.Errorf("manager %d: service file names\n%s\nwant %s", m, data, stable)
		}
	}
}

func TestServiceRestartsOnExitUpgrade(t *testing.T) {
	plist, err := renderPlist("/usr/local/bin/togo", "/home/u/.togo", "/home/u/.togo/log/daemon.log")
	if err != nil {
		t.Fatal(err)
	}
	// launchd restarts a job whose exit was not successful; 75 is not.
	if !strings.Contains(string(plist), "<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>") {
		t.Errorf("plist does not restart on a failed exit:\n%s", plist)
	}
	unit := renderUnit("/usr/local/bin/togo", "/home/u/.togo")
	if !strings.Contains(unit, "\nRestart=on-failure\n") {
		t.Errorf("unit does not restart on a failed exit:\n%s", unit)
	}
	// Nothing may mark 75 as a clean exit or forbid restarting on it.
	for _, banned := range []string{"SuccessExitStatus", "RestartPreventExitStatus"} {
		if strings.Contains(unit, banned) {
			t.Errorf("unit sets %s", banned)
		}
	}
	if ExitUpgrade != 75 {
		t.Errorf("ExitUpgrade = %d, want 75", ExitUpgrade)
	}
}

// pipeOf returns the pane_pipe flag and recorded tap command of a session.
func pipeOf(t *testing.T, c *tmux.Client, id string) (bool, string) {
	t.Helper()
	piped, err := c.PipedSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cmds, err := c.TapCommands(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return piped[id], cmds[id]
}

func TestDaemonStartRearmsTaps(t *testing.T) {
	c := tmuxtest.New(t)
	home := shortHome(t)
	ctx := context.Background()
	const (
		unpiped = "s_aaaaaaaaaa" // no tap at all
		moved   = "s_bbbbbbbbbb" // tap names a binary path that is gone
		current = "s_cccccccccc" // tap already runs this binary
	)
	for i, id := range []string{unpiped, moved, current} {
		if out, err := exec.Command(c.Bin, append(c.Socket, "new-session", "-d", "-s", id, "sleep", "60")...).CombinedOutput(); err != nil {
			t.Fatalf("new-session: %v %s", err, out)
		}
		if err := c.SetOption(ctx, id, "@togo_bib", fmt.Sprintf("%02d", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	// A stand-in helper that logs each tap it starts and reads the pane.
	starts := filepath.Join(home, "starts")
	exe := filepath.Join(home, "togo")
	script := "#!/bin/sh\necho \"$2\" >>'" + starts + "'\nexec cat >/dev/null\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	command := func(bin, id string) string {
		cmd, err := tap.PipeCommand(bin, id)
		if err != nil {
			t.Fatal(err)
		}
		return "TOGO_HOME='" + home + "' " + cmd
	}
	// Taps left by an earlier daemon: one recorded with the Homebrew
	// version path an upgrade removed, one with this binary's path.
	for id, recorded := range map[string]string{
		moved:   command("/opt/homebrew/Cellar/togo/1.0.0/bin/togo", moved),
		current: command(exe, current),
	} {
		if err := c.PipePane(ctx, "="+id+":", "exec cat >/dev/null"); err != nil {
			t.Fatal(err)
		}
		if err := c.SetOption(ctx, id, tmux.TapOption, recorded); err != nil {
			t.Fatal(err)
		}
	}

	comp, err := Compose(&api.API{Sessions: &session.Service{Tmux: c}}, ComposeOptions{Home: home, TogoPath: exe, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	comp.Watch.Reconcile(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(starts)
		got := strings.Fields(string(data))
		if slices.Contains(got, unpiped) && slices.Contains(got, moved) {
			if slices.Contains(got, current) {
				t.Errorf("tap started again for %s, whose tap already ran this binary", current)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("taps started for %q, want %s and %s", got, unpiped, moved)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, id := range []string{unpiped, moved, current} {
		if piped, cmd := pipeOf(t, c, id); !piped || cmd != command(exe, id) {
			t.Errorf("%s: piped=%v tap=%q, want %q", id, piped, cmd, command(exe, id))
		}
	}
}
