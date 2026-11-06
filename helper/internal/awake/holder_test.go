package awake

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeProcess struct {
	once   sync.Once
	exited chan struct{}
	err    error
	killed bool
}

func newFakeProcess() *fakeProcess { return &fakeProcess{exited: make(chan struct{})} }

func (p *fakeProcess) Wait() error {
	<-p.exited
	return p.err
}

func (p *fakeProcess) Kill() error {
	p.killed = true
	p.exit(errors.New("signal: killed"))
	return nil
}

func (p *fakeProcess) exit(err error) {
	p.once.Do(func() {
		p.err = err
		close(p.exited)
	})
}

type fakeExec struct {
	mu     sync.Mutex
	argvs  [][]string
	procs  []*fakeProcess
	refuse error
}

func (f *fakeExec) start(argv []string) (Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return nil, f.refuse
	}
	p := newFakeProcess()
	f.argvs = append(f.argvs, argv)
	f.procs = append(f.procs, p)
	return p, nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLinuxInhibitorStartedAndStopped(t *testing.T) {
	fx := &fakeExec{}
	h := Inhibitor(4242, "/usr/bin/systemd-inhibit", "/usr/bin/tail")
	h.Start = fx.start
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/usr/bin/systemd-inhibit", "--what=sleep:idle:handle-lid-switch", "--mode=block",
		"--who=Togo", "--why=Togo sessions are working",
		"/usr/bin/tail", "--pid=4242", "-f", "/dev/null",
	}
	if len(fx.argvs) != 1 || !slices.Equal(fx.argvs[0], want) {
		t.Fatalf("started %q, want one %q", fx.argvs, want)
	}
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}
	if !fx.procs[0].killed {
		t.Fatal("release did not stop the inhibitor")
	}
	if err := h.Err(); err != nil {
		t.Fatalf("a released child reported %v", err)
	}
}

func TestCaffeinateArgv(t *testing.T) {
	fx := &fakeExec{}
	h := Caffeinate(77)
	h.Start = fx.start
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/caffeinate", "-s", "-i", "-w", "77"}
	if !slices.Equal(fx.argvs[0], want) {
		t.Fatalf("argv %q, want %q", fx.argvs[0], want)
	}
	h.Release()
}

func TestChildExitIsReportedAndRestarted(t *testing.T) {
	fx := &fakeExec{}
	h := Inhibitor(1, "/usr/bin/systemd-inhibit", "/usr/bin/tail")
	h.Start = fx.start
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	fx.procs[0].exit(errors.New("Failed to inhibit: Access denied"))
	waitFor(t, func() bool { return h.Err() != nil })
	if got := h.Err().Error(); got != "Failed to inhibit: Access denied" {
		t.Fatalf("Err = %q", got)
	}
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	if len(fx.argvs) != 2 || h.Err() != nil {
		t.Fatalf("starts %d err %v, want a fresh child", len(fx.argvs), h.Err())
	}
	h.Release()
}

func TestStartFailureIsHoldError(t *testing.T) {
	fx := &fakeExec{refuse: errors.New("no such file")}
	h := Caffeinate(1)
	h.Start = fx.start
	if err := h.Hold(); err == nil {
		t.Fatal("Hold succeeded without a child")
	}
}

// TestReleaseKillsGrandchildren runs a harmless shell that starts a sleep,
// standing in for systemd-inhibit and the tail it runs.
func TestReleaseKillsGrandchildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	h := &Child{Argv: []string{"/bin/sh", "-c", "/bin/sleep 60 & echo $! > " + pidFile + "; wait"}}
	if err := h.Hold(); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return syscall.Kill(pid, 0) != nil })
}
