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
// login profiles print (a motd, an echo in .zprofile) is not mistaken for it.
const loginPathMarker = "\x1eTOGO_PATH="

// LoginPath returns PATH as the user's login shell sets it. The helper may
// run under launchd or a bare ssh command with a minimal PATH, so agent
// lookups use this instead. The result is cached for the process, except a
// fallback caused by ctx ending, which is retried on the next call.
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

// loginPath asks shell for its login PATH, falling back to the process PATH.
// cacheable is false when the fallback came from ctx being cancelled or
// timing out rather than from the shell itself.
func loginPath(ctx context.Context, shell string) (path string, cacheable bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", `printf '\036TOGO_PATH=%s\036' "$PATH"`).Output()
	if err == nil {
		if _, rest, ok := strings.Cut(string(out), loginPathMarker); ok {
			if p, _, ok := strings.Cut(rest, "\x1e"); ok && p != "" {
				return p, true
			}
		}
	}
	return os.Getenv("PATH"), ctx.Err() == nil
}
