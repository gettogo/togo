package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func quickSteps(logged *[]string) exitSteps {
	return exitSteps{
		drain: func() bool { return true }, stop: func() {}, ran: closed(), flush: func() {},
		hookWait: time.Second, serviceWait: time.Second, flushWait: time.Second,
		logf: func(f string, a ...any) { *logged = append(*logged, fmt.Sprintf(f, a...)) },
	}
}

func closed() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// finishWithin runs finish and fails the test if it outlives limit.
func finishWithin(t *testing.T, s exitSteps, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() { finish(s); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("finish still runs after %v", limit)
	}
	return time.Since(start)
}

// A helper on its way out finishes in order: the hooks it accepted are
// read and stored, then the services stop, then the open approvals reach
// disk.
func TestFinishDrainsHooksThenStopsThenFlushes(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	var logged []string
	s := quickSteps(&logged)
	ran := make(chan struct{})
	s.drain = func() bool { note("drain"); return true }
	s.stop = func() {
		note("stop")
		go func() { time.Sleep(20 * time.Millisecond); note("services ended"); close(ran) }()
	}
	s.ran = ran
	s.flush = func() { note("flush") }
	finishWithin(t, s, 5*time.Second)
	if got := strings.Join(order, ","); got != "drain,stop,services ended,flush" {
		t.Fatalf("exit order %s", got)
	}
	if len(logged) != 0 {
		t.Errorf("a clean exit logged %q", logged)
	}
}

// Every step is bounded: hooks still being read, services that do not
// end and a flush that never returns (a full disk) do not keep the
// process alive, and one line says what was left unfinished.
func TestFinishIsBoundedAtEveryStep(t *testing.T) {
	var logged []string
	s := quickSteps(&logged)
	never := make(chan struct{})
	s.drain = func() bool { <-never; return true }
	s.ran = make(chan struct{})
	s.flush = func() { <-never }
	s.hookWait, s.serviceWait, s.flushWait = 30*time.Millisecond, 30*time.Millisecond, 30*time.Millisecond
	if d := finishWithin(t, s, 2*time.Second); d > time.Second {
		t.Fatalf("finish took %v", d)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "hook events") || !strings.Contains(logged[0], "services") || !strings.Contains(logged[0], "approvals") {
		t.Fatalf("log %q, want one line naming the hooks, the services and the approvals", logged)
	}
}

// A drain that reports it ran out is named too.
func TestFinishLogsADrainThatRanOut(t *testing.T) {
	var logged []string
	s := quickSteps(&logged)
	s.drain = func() bool { return false }
	finishWithin(t, s, 2*time.Second)
	if len(logged) != 1 || !strings.Contains(logged[0], "hook events") {
		t.Fatalf("log %q", logged)
	}
}

// A second stop request while the helper drains skips the rest of the
// drain and of the services' wait; the approvals are still written.
func TestFinishHurriesOnASecondStop(t *testing.T) {
	var logged []string
	s := quickSteps(&logged)
	never := make(chan struct{})
	s.drain = func() bool { <-never; return true }
	s.ran = make(chan struct{})
	s.hookWait, s.serviceWait = time.Minute, time.Minute
	flushed := false
	s.flush = func() { flushed = true }
	hurry := make(chan struct{})
	s.hurry = hurry
	time.AfterFunc(30*time.Millisecond, func() { close(hurry) })
	if d := finishWithin(t, s, 5*time.Second); d > 2*time.Second {
		t.Fatalf("finish took %v after the second stop", d)
	}
	if !flushed {
		t.Error("the approvals were not written")
	}
}

// The services run on a context the stop signal does not end: the
// signal cancels the daemon's context before the exit steps run, and the
// drain needs the hook readers and their sessions still working. The exit
// steps end them.
func TestStartKeepsServicesPastTheSignal(t *testing.T) {
	signal, cancel := context.WithCancel(context.Background())
	got := make(chan context.Context, 1)
	stop, ran := startRun(signal, func(ctx context.Context) { got <- ctx; <-ctx.Done() })
	ctx := <-got
	cancel()
	time.Sleep(20 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("the services stopped with the signal, before the drain")
	}
	stop()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the services did not end once stopped")
	}
}

// The bounds add up to well under launchd's 20 seconds before it kills a
// job that does not exit.
func TestExitBoundsFitTheServiceManagers(t *testing.T) {
	if total := exitHookWait + exitServiceWait + exitFlushWait; total > 10*time.Second {
		t.Fatalf("exit bounds total %v", total)
	}
}
