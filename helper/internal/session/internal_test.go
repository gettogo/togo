package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func TestNewIDShape(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := newID()
		if managed, err := ValidateID(id); err != nil || !managed {
			t.Fatalf("newID() = %q does not validate as managed (%v)", id, err)
		}
		seen[id] = true
	}
	if len(seen) < 100 {
		t.Fatalf("100 ids produced only %d distinct values", len(seen))
	}
}

func TestValidateID(t *testing.T) {
	cases := []struct {
		id      string
		managed bool
		valid   bool
	}{
		{"s_abcdefghij", true, true},
		{"s_abcdefgh27", true, true},
		{"u_6d7920776f726b", false, true},
		{"s_abcdefghi", false, false},
		{"s_ABCDEFGHIJ", false, false},
		{"s_abcdefghi1", false, false},
		{"u_", false, false},
		{"u_abc", false, false},
		{"u_6D", false, false},
		{"", false, false},
		{"01", false, false},
	}
	for _, tc := range cases {
		managed, err := ValidateID(tc.id)
		var ipe *InvalidParamsError
		if tc.valid && (err != nil || managed != tc.managed) {
			t.Errorf("ValidateID(%q) = %v, %v; want %v, nil", tc.id, managed, err, tc.managed)
		}
		if !tc.valid && !errors.As(err, &ipe) {
			t.Errorf("ValidateID(%q) error = %v, want *InvalidParamsError", tc.id, err)
		}
	}
}

func TestUnmanagedIDRoundTrip(t *testing.T) {
	for _, name := range []string{"my work", "a", "émoji 🐕", `my\nwork`} {
		id := unmanagedID(name)
		got, err := unmanagedName(id)
		if err != nil || got != name {
			t.Errorf("unmanagedName(unmanagedID(%q)) = %q, %v", name, got, err)
		}
	}
}

func TestLowestFreeBib(t *testing.T) {
	if got, _ := lowestFreeBib(map[string]bool{}); got != "01" {
		t.Errorf("empty: got %s, want 01", got)
	}
	if got, _ := lowestFreeBib(map[string]bool{"01": true, "03": true}); got != "02" {
		t.Errorf("01,03 used: got %s, want 02", got)
	}
	all := map[string]bool{}
	for i := 1; i <= 99; i++ {
		all[fmt.Sprintf("%02d", i)] = true
	}
	_, err := lowestFreeBib(all)
	var ipe *InvalidParamsError
	if !errors.As(err, &ipe) || ipe.Msg != "all 99 session numbers in use" {
		t.Fatalf("all used: error = %v, want InvalidParamsError(all 99 session numbers in use)", err)
	}
}

func TestAgentCommand(t *testing.T) {
	shell, _ := agents.Lookup("shell")
	if got := agentCommand(shell, "/bin/zsh"); got != nil {
		t.Errorf("shell agent command = %v, want nil so tmux runs the default shell", got)
	}
	claude, _ := agents.Lookup("claude")
	want := []string{"/bin/zsh", "-lic", `claude; exec "$SHELL" -l`}
	if got := agentCommand(claude, "/bin/zsh"); !reflect.DeepEqual(got, want) {
		t.Errorf("claude command = %v, want %v", got, want)
	}
}

func TestTmuxUnavailableErrorMentionsRequired(t *testing.T) {
	for _, e := range []*TmuxUnavailableError{{Found: "2.9"}, {}} {
		if !strings.Contains(e.Error(), "3.2") {
			t.Errorf("Error() = %q, want it to mention 3.2", e.Error())
		}
	}
}

func TestNewServiceProduction(t *testing.T) {
	label := fmt.Sprintf("togo-test-%d-newservice", os.Getpid())
	t.Setenv("TOGO_TMUX_LABEL", label)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", label, "kill-server").Run()
		_ = os.Remove(tmuxtest.SocketPath(label))
	})
	home := t.TempDir()

	s, err := NewService(context.Background(), home)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	conf := filepath.Join(home, "tmux.conf")
	if !reflect.DeepEqual(s.Tmux.Socket, []string{"-L", label}) || s.Tmux.Config != conf {
		t.Errorf("Tmux = %+v, want label %s with config %s", s.Tmux, label, conf)
	}
	if s.User == nil || s.User.Socket != nil {
		t.Errorf("User = %+v, want the default tmux server", s.User)
	}
	if s.Recent.File != filepath.Join(home, "recent.json") || s.LockFile != filepath.Join(home, "run", "bib.lock") || s.Shell != "/bin/sh" {
		t.Errorf("NewService fields = recent %s, lock %s, shell %s", s.Recent.File, s.LockFile, s.Shell)
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("ReadFile tmux.conf: %v", err)
	}
	for _, line := range []string{
		"set -g mouse on", "set -g set-clipboard on", "set -s escape-time 0",
		"set -g history-limit 50000", `set -g default-terminal "tmux-256color"`,
	} {
		if !strings.Contains(string(data), line+"\n") {
			t.Errorf("tmux.conf lacks %q", line)
		}
	}
	if info, err := os.Stat(conf); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("tmux.conf mode = %v, %v; want 600", info, err)
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	if !filepath.IsAbs(s.Tmux.Bin) || s.User.Bin != s.Tmux.Bin {
		t.Errorf("tmux binaries = %q and %q, want one absolute path", s.Tmux.Bin, s.User.Bin)
	}
	// The real server must have loaded the file; User is left alone so the
	// owner's default socket is never touched.
	s.User = nil
	created, err := s.Create(context.Background(), CreateOpts{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	out, err := exec.Command("tmux", "-L", label, "show-options", "-gv", "history-limit").Output()
	if err != nil || strings.TrimSpace(string(out)) != "50000" {
		t.Fatalf("history-limit = %q, %v; want 50000 for session %s", out, err, created.ID)
	}
}

func TestWithLockHonoursContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "bib.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	holder, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	ran := false
	err = withLock(ctx, path, func() error { ran = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || ran {
		t.Fatalf("withLock on a held lock = %v (ran %v), want DeadlineExceeded without running fn", err, ran)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("withLock returned after %v, want soon after the deadline", waited)
	}

	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := withLock(context.Background(), path, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("withLock on a free lock = %v (ran %v), want fn run", err, ran)
	}
}

func fakeExecutable(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tmux")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveTmuxPrefersLoginPath(t *testing.T) {
	root := t.TempDir()
	login := fakeExecutable(t, filepath.Join(root, "login"), 0o755)
	fallback := fakeExecutable(t, filepath.Join(root, "fallback"), 0o755)
	path := strings.Join([]string{filepath.Join(root, "empty"), filepath.Join(root, "login")}, string(os.PathListSeparator))
	if got := resolveTmux(path, []string{fallback}); got != login {
		t.Fatalf("resolveTmux = %q, want the login PATH match %q", got, login)
	}
}

func TestResolveTmuxFallsBackToKnownLocations(t *testing.T) {
	root := t.TempDir()
	notExec := fakeExecutable(t, filepath.Join(root, "noexec"), 0o644)
	fallback := fakeExecutable(t, filepath.Join(root, "brew"), 0o755)
	// A relative entry would depend on the helper's working directory.
	t.Chdir(root)
	path := strings.Join([]string{"/usr/bin/nonexistent-dir", "brew", filepath.Join(root, "noexec")}, string(os.PathListSeparator))
	if got := resolveTmux(path, []string{filepath.Join(root, "missing", "tmux"), notExec, fallback}); got != fallback {
		t.Fatalf("resolveTmux = %q, want the first executable fallback %q", got, fallback)
	}
}

func TestResolveTmuxKeepsBareNameWhenMissing(t *testing.T) {
	if got := resolveTmux("", []string{filepath.Join(t.TempDir(), "tmux")}); got != "tmux" {
		t.Fatalf("resolveTmux with no tmux anywhere = %q, want %q so the not-installed path reports it", got, "tmux")
	}
}
