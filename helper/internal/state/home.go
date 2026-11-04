// Package state owns the helper's files under $TOGO_HOME and facts about the
// host user's environment that are costly to compute.
package state

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Home returns $TOGO_HOME, or ~/.togo when unset, creating it with mode 0700
// and tightening an existing directory to 0700.
func Home() (string, error) {
	dir := os.Getenv("TOGO_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("togo home: %w", err)
		}
		dir = filepath.Join(home, ".togo")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	return dir, nil
}

// WriteFile replaces path with data, mode 0600. It writes a temporary file in
// the same directory, syncs it and renames it, so readers never see a
// partial file, even after a power loss.
func WriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

var (
	loginPathMu     sync.Mutex
	loginPathCached string
)

// loginPathMarker frames the PATH value in the shell's output, so text that
// login and rc files print (a motd, an echo in .zshrc) is not mistaken for
// it.
const loginPathMarker = "\x1eTOGO_PATH="

// loginPathTimeout bounds the shell lookup. A variable so tests can shorten
// it.
var loginPathTimeout = 2 * time.Second

// LoginPath returns PATH as the user's interactive login shell sets it, the
// same "$SHELL -lic" agents are launched with. The helper may run under
// launchd or a bare ssh command with a minimal PATH, so agent and tmux
// lookups use this instead. The result is cached for the process, including
// the process PATH used when the shell fails or hangs past the timeout. Only
// a fallback caused by the caller's ctx ending is retried on the next call.
func LoginPath(ctx context.Context) string {
	loginPathMu.Lock()
	defer loginPathMu.Unlock()
	if loginPathCached != "" {
		return loginPathCached
	}
	p, cacheable := loginPath(ctx, cmp.Or(os.Getenv("SHELL"), "/bin/sh"))
	if cacheable {
		loginPathCached = p
	}
	return p
}

// loginPath asks shell for its interactive login PATH, falling back to the
// process PATH. cacheable is false when the fallback came from the caller's
// ctx ending rather than from the shell itself.
func loginPath(ctx context.Context, shell string) (path string, cacheable bool) {
	tctx, cancel := context.WithTimeout(ctx, loginPathTimeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, shell, "-lic", `printf '\036TOGO_PATH=%s\036' "$PATH"`)
	// Stdin stays nil, which is /dev/null, so an rc file that reads input
	// sees EOF instead of blocking. A new session detaches the shell from
	// any controlling terminal the helper has, so interactive job control
	// cannot stop it, and makes it a process group that the timeout kills
	// whole: a child left holding stdout would otherwise keep Output waiting.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err == nil {
		if _, rest, ok := strings.Cut(string(out), loginPathMarker); ok {
			if p, _, ok := strings.Cut(rest, "\x1e"); ok && p != "" {
				return p, true
			}
		}
	}
	return os.Getenv("PATH"), ctx.Err() == nil
}
