package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// hungShell never prints and never exits: the .zprofile line that hung
// under launchd.
func hungShell(t *testing.T) string {
	return writeShell(t, "#!/bin/sh\n/bin/sleep 30\n")
}

func shortTimeouts(t *testing.T, read, first time.Duration) {
	oldRead, oldFirst := loginPathTimeout, loginFirstWait
	loginPathTimeout, loginFirstWait = read, first
	t.Cleanup(func() { loginPathTimeout, loginFirstWait = oldRead, oldFirst })
}

// A good read is kept on disk, 0600, and a later helper whose shell hangs
// starts with it at once instead of the bare process PATH.
func TestLoginShellPersistsLastGoodRead(t *testing.T) {
	resetLoginPath(t)
	dir := t.TempDir()
	PersistLoginShell(dir)
	t.Setenv("PATH", "/proc/bin")
	t.Setenv("SHELL", writeShell(t, "#!/bin/sh\nPATH=/login/bin:/usr/bin\nexport MOONSHOT_API_KEY=k1\neval \"$2\"\n"))
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath = %q, want the login PATH", got)
	}
	file := filepath.Join(dir, loginCacheFile)
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache file = %v, %v; want it written 0600", info, err)
	}

	// A new process: nothing in memory, and the shell now hangs.
	login.mu.Lock()
	login.env, login.fresh, login.loaded = nil, false, false
	login.mu.Unlock()
	shortTimeouts(t, 300*time.Millisecond, 5*time.Second)
	t.Setenv("SHELL", hungShell(t))
	start := time.Now()
	p, fromShell := LoginPathSource(context.Background())
	if p != "/login/bin:/usr/bin" || !fromShell {
		t.Fatalf("LoginPathSource with a hung shell = %q, %v; want the kept read", p, fromShell)
	}
	if !slices.Contains(LoginEnv(context.Background()), "MOONSHOT_API_KEY=k1") {
		t.Fatal("LoginEnv lost the kept key")
	}
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Fatalf("a kept read took %v; it must not wait for the shell", waited)
	}
	waitLoginRead(t) // the refresh it started fails and keeps the old read
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath after a failed refresh = %q, want the kept read", got)
	}
}

// With nothing read yet a hung shell holds a caller at most loginFirstWait.
func TestLoginShellFirstWaitIsBounded(t *testing.T) {
	resetLoginPath(t)
	shortTimeouts(t, 2*time.Second, 150*time.Millisecond)
	t.Setenv("PATH", "/proc/bin")
	t.Setenv("SHELL", hungShell(t))
	start := time.Now()
	if got := LoginPath(context.Background()); got != "/proc/bin" {
		t.Fatalf("LoginPath = %q, want the process PATH", got)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("LoginPath waited %v for a hung shell", waited)
	}
	// A second caller does not start a second shell or wait longer.
	start = time.Now()
	_ = LoginEnv(context.Background())
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("LoginEnv waited %v", waited)
	}
}

// A failed read schedules a background retry that backs off, and the retry
// reads the shell again.
func TestLoginShellRetriesInBackground(t *testing.T) {
	resetLoginPath(t)
	shortTimeouts(t, 200*time.Millisecond, 2*time.Second)
	var mu sync.Mutex
	var delays []time.Duration
	var retry func()
	retryLater = func(d time.Duration, f func()) {
		mu.Lock()
		delays = append(delays, d)
		retry = f
		mu.Unlock()
	}
	t.Setenv("PATH", "/proc/bin")
	t.Setenv("SHELL", hungShell(t))
	_ = LoginPath(context.Background())
	waitLoginRead(t)
	mu.Lock()
	if len(delays) != 1 || delays[0] != loginPathRetry {
		t.Fatalf("retries = %v, want one after %v", delays, loginPathRetry)
	}
	f := retry
	mu.Unlock()
	t.Setenv("SHELL", fakeShell(t))
	f()
	waitLoginRead(t)
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath after the retry = %q, want the login PATH", got)
	}
}

func TestRetryDelayBacksOff(t *testing.T) {
	if retryDelay(1) != loginPathRetry || retryDelay(2) != 2*loginPathRetry || retryDelay(50) != loginRetryMax {
		t.Fatalf("delays = %v %v %v", retryDelay(1), retryDelay(2), retryDelay(50))
	}
}

// The shell runs with TERM set (launchd gives none) and stdin at EOF.
func TestLoginShellSetsTermAndNoStdin(t *testing.T) {
	t.Setenv("TERM", "")
	shell := writeShell(t, "#!/bin/sh\nread line && export GOT_STDIN=1\neval \"$2\"\n")
	env, ok := loginEnv(context.Background(), shell)
	if !ok {
		t.Fatal("loginEnv failed")
	}
	if !slices.Contains(env, "TERM=xterm-256color") {
		t.Fatalf("TERM not set: %v", env)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOT_STDIN=") {
			t.Fatal("the shell read something from stdin")
		}
	}
}

// A cache file that does not parse or holds no PATH is ignored.
func TestLoginShellIgnoresBadCache(t *testing.T) {
	resetLoginPath(t)
	dir := t.TempDir()
	bad, _ := json.Marshal(loginCache{Env: []string{"A=1"}})
	if err := os.WriteFile(filepath.Join(dir, loginCacheFile), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	PersistLoginShell(dir)
	t.Setenv("PATH", "/proc/bin")
	t.Setenv("SHELL", "/nonexistent/shell")
	if got := LoginPath(context.Background()); got != "/proc/bin" {
		t.Fatalf("LoginPath = %q, want the process PATH", got)
	}
}
