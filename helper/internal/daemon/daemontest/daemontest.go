// Package daemontest stops helper daemons that tests started, directly or
// through `togo serve --stdio`, which detaches the daemon it starts.
package daemontest

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/sockd"
)

// Stop ends the daemon serving home, if one runs, and waits until it has
// let go of its lock. It sends SIGTERM, then SIGKILL after five seconds,
// to the daemon's process group when the daemon leads one. It returns the
// pid it stopped, or 0 when no daemon held the lock.
func Stop(t testing.TB, home string) int {
	t.Helper()
	if !Running(home) {
		return 0
	}
	raw, err := os.ReadFile(daemon.PIDPath(home))
	if err != nil {
		t.Errorf("daemon holds the lock for %s but its pid is unreadable: %v", home, err)
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		t.Errorf("daemon pid file for %s holds %q", home, raw)
		return 0
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		signal(pid, sig)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !Running(home) && !Alive(pid) {
				return pid
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Errorf("daemon %d for %s survived SIGKILL", pid, home)
	return pid
}

// signal sends sig to pid's process group when pid leads it, as a daemon
// started by serve does, and to pid alone otherwise, so a daemon that
// shares the test's group never takes the test down with it.
func signal(pid int, sig syscall.Signal) {
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		_ = syscall.Kill(-pid, sig)
		return
	}
	_ = syscall.Kill(pid, sig)
}

// Running reports whether a daemon holds the lock for home.
func Running(home string) bool {
	release, err := sockd.Lock(daemon.LockPath(home))
	if err == nil {
		release()
		return false
	}
	return errors.Is(err, sockd.ErrLocked)
}

// Alive reports whether a process with pid exists. A zombie counts as
// gone only once its parent reaps it.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
