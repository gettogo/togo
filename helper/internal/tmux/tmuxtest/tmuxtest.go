// Package tmuxtest provides helpers for tests that drive a real tmux server
// on a private socket, keeping the production code free of testing imports.
package tmuxtest

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
)

var (
	counter   uint64
	sweepOnce sync.Once
)

// New returns a Client bound to a unique private socket label and registers
// cleanup that stops the server on that label, waits until the processes of
// its panes are gone (a pane's shell or agent may still be writing into the
// test's folders just after kill-server returns), and deletes its socket
// file. The first call in a process also stops the servers that earlier,
// killed test runs left behind (SweepStale). The test is skipped when tmux
// is not on PATH. The returned client never uses the default socket.
func New(t testing.TB) *tmux.Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sweepOnce.Do(func() { SweepStale(tmuxBin()) })
	label := fmt.Sprintf("togo-test-%d-%d", os.Getpid(), atomic.AddUint64(&counter, 1))
	c := tmux.NewLabel(label)
	t.Cleanup(func() { stop(c.Bin, label) })
	return c
}

// tmuxBin is the tmux program the sweep uses.
func tmuxBin() string {
	p, _ := exec.LookPath("tmux")
	return p
}

// stop ends the server on label and what ran in its panes.
func stop(bin, label string) {
	pids := paneTree(bin, label)
	_ = exec.Command(bin, "-L", label, "kill-server").Run()
	reap(pids, 5*time.Second)
	// tmux 3.6 leaves the socket file behind after kill-server.
	_ = os.Remove(SocketPath(label))
}

// paneTree lists the panes' processes of the server on label and
// everything they started.
func paneTree(bin, label string) []int {
	out, err := exec.Command(bin, "-L", label, "list-panes", "-a", "-F", "#{pane_pid}").Output()
	if err != nil {
		return nil
	}
	var roots []int
	for _, f := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(f); err == nil && n > 1 {
			roots = append(roots, n)
		}
	}
	return withDescendants(roots)
}

func withDescendants(roots []int) []int {
	if len(roots) == 0 {
		return nil
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return roots
	}
	kids := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 == nil && e2 == nil {
			kids[ppid] = append(kids[ppid], pid)
		}
	}
	all := append([]int(nil), roots...)
	for i := 0; i < len(all); i++ {
		all = append(all, kids[all[i]]...)
	}
	return all
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// reap waits for the processes to end, killing the ones that outlast a
// fifth of the limit.
func reap(pids []int, limit time.Duration) {
	start := time.Now()
	killed := false
	for {
		left := 0
		for _, p := range pids {
			if alive(p) {
				left++
				if !killed && time.Since(start) > limit/5 {
					_ = syscall.Kill(p, syscall.SIGKILL)
				}
			}
		}
		if left == 0 || time.Since(start) > limit {
			return
		}
		if time.Since(start) > limit/5 {
			killed = true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// SweepStale stops the tmux servers on togo-test sockets whose test process
// is gone: a run that was killed or timed out never ran its cleanup. A
// server whose process is alive belongs to a test running now and is left
// alone. TOGO_TEST_NO_SWEEP=1 turns it off.
func SweepStale(bin string) {
	if bin == "" || os.Getenv("TOGO_TEST_NO_SWEEP") != "" {
		return
	}
	socks, _ := filepath.Glob(filepath.Join(filepath.Dir(SocketPath("x")), "togo-test-*"))
	for _, s := range socks {
		parts := strings.Split(strings.TrimPrefix(filepath.Base(s), "togo-test-"), "-")
		pid, err := strconv.Atoi(parts[0])
		if err != nil || alive(pid) {
			continue
		}
		stop(bin, filepath.Base(s))
	}
}

// SocketPath returns where tmux puts the socket for label:
// $TMUX_TMPDIR (default /tmp), then tmux-UID, then the label.
func SocketPath(label string) string {
	dir := cmp.Or(os.Getenv("TMUX_TMPDIR"), "/tmp")
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), label)
}
