package daemon

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// A helper on its way out (`togo update`, SIGTERM) finishes in order: the
// hooks it accepted are read and stored, then the services stop, then the
// open approvals reach disk. The process exits only after that, whichever
// way it was asked to stop.
func TestFinishDrainsHooksThenStopsThenFlushes(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	ran := make(chan struct{})
	finish(exitSteps{
		drain: func() bool { note("drain"); return true },
		stop: func() {
			note("stop")
			go func() { time.Sleep(20 * time.Millisecond); note("services ended"); close(ran) }()
		},
		ran:   ran,
		flush: func() { note("flush") },
		wait:  5 * time.Second,
		logf:  func(string, ...any) {},
	})
	if got := strings.Join(order, ","); got != "drain,stop,services ended,flush" {
		t.Fatalf("exit order %s", got)
	}
}

// A service that does not end, or hooks still being read when the bound
// passes, do not keep the approvals off disk, and the log says so.
func TestFinishFlushesWhenServicesHang(t *testing.T) {
	var logged []string
	flushed := false
	start := time.Now()
	finish(exitSteps{
		drain: func() bool { return false },
		stop:  func() {},
		ran:   make(chan struct{}),
		flush: func() { flushed = true },
		wait:  30 * time.Millisecond,
		logf:  func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) },
	})
	if !flushed {
		t.Fatal("the approvals were not flushed")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("finish took %v", d)
	}
	if len(logged) != 2 {
		t.Fatalf("log %q, want a line for the hooks and one for the services", logged)
	}
}
