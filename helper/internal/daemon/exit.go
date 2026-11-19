package daemon

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Bounds on a helper's way out: how long it waits for the hooks it
// accepted to be read, and for its services to end.
const (
	exitHookWait    = 5 * time.Second
	exitServiceWait = 5 * time.Second
)

// Start runs the background services (Run) and returns the function the
// daemon calls once it has stopped serving, before the process exits.
//
// The process exits as soon as the daemon returns, on every path: a
// restart for `togo update` (the context is not cancelled then) and
// SIGTERM alike. Without this, the services' context was never cancelled
// on a restart, so Run's last write of the open approvals never ran, and
// on SIGTERM it raced the exit. And a hook's `togo hook` command has its
// answer once the payload is queued for its feed, so payloads still queued
// at the exit were lost: a hook-fed agent's chat (engine/hookstore.go)
// came back after the restart without them.
func (c *Components) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		c.Run(ctx)
	}()
	return func() {
		finish(exitSteps{
			drain: func() bool { return source.Hooks.Drain("", exitHookWait) },
			stop:  cancel,
			ran:   ran,
			flush: c.Approvals.Flush,
			wait:  exitServiceWait,
			logf:  c.logf,
		})
	}
}

type exitSteps struct {
	drain func() bool   // waits for the accepted hooks to be read
	stop  func()        // ends the services
	ran   chan struct{} // closed when they have ended
	flush func()        // writes the open approvals
	wait  time.Duration // bound on the services ending
	logf  func(string, ...any)
}

// finish runs a helper's way out in order. The daemon has stopped
// serving, so no hook arrives after the drain.
func finish(s exitSteps) {
	if !s.drain() {
		s.logf("daemon: exiting with hook events not yet read")
	}
	s.stop()
	select {
	case <-s.ran:
	case <-time.After(s.wait):
		s.logf("daemon: exiting before every service ended")
	}
	// Run flushes too once its services end; flushing again is a no-op.
	s.flush()
}
