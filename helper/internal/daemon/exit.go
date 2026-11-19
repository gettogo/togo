package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Bounds on a helper's way out, run inside Run while it still holds the
// lock (Config.Finish): how long it waits for the hooks it accepted to be
// read and stored, for its services to end, and for the open approvals to
// be written. Together they stay well under the 20 s launchd gives a job
// to exit after SIGTERM before it kills it (systemd gives 90 s), and they
// bound a restart, where nothing would kill a helper that hung.
//
// When a bound is hit the helper exits anyway, with the status it would
// have had, and logs one line naming what was not finished. Hook events
// not yet read are then missing from the restored chat (the agent and its
// later hooks are not affected). Approvals not yet written leave the file
// as it was last written: on the next start each kept approval is checked
// against its pane, one whose prompt is gone is closed, and a prompt still
// on screen that the file lacks is opened again when the pane watcher
// reads it.
const (
	exitHookWait    = 4 * time.Second
	exitServiceWait = 3 * time.Second
	exitFlushWait   = 2 * time.Second
)

// Start runs the background services (Run) and returns the exit steps the
// daemon runs once it has stopped serving and before it lets go of its
// lock (Config.Finish). A close of hurry (a second stop request) cuts the
// waits for hooks and services short.
//
// The services run on a context the stop signal does not cancel: on
// SIGTERM the daemon's context ends before the exit steps run, and the
// hooks must still be read first. The exit steps end the services.
func (c *Components) Start(ctx context.Context) (finishing func(hurry <-chan struct{})) {
	stop, ran := startRun(ctx, c.Run)
	return func(hurry <-chan struct{}) {
		finish(exitSteps{
			drain:       func() bool { return source.Hooks.Drain("", exitHookWait) },
			stop:        stop,
			ran:         ran,
			flush:       c.Approvals.Flush,
			hookWait:    exitHookWait,
			serviceWait: exitServiceWait,
			flushWait:   exitFlushWait,
			hurry:       hurry,
			logf:        c.logf,
		})
	}
}

// startRun runs run on a context only stop ends; ran closes when it
// returns.
func startRun(ctx context.Context, run func(context.Context)) (stop func(), ran <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx)
	}()
	return cancel, done
}

type exitSteps struct {
	drain       func() bool     // waits for the accepted hooks to be read
	stop        func()          // ends the services
	ran         <-chan struct{} // closed when they have ended
	flush       func()          // writes the open approvals
	hookWait    time.Duration
	serviceWait time.Duration
	flushWait   time.Duration
	hurry       <-chan struct{} // closed by a second stop request
	logf        func(string, ...any)
}

// finish runs a helper's way out in order, each step bounded. The daemon
// has stopped serving, so no hook arrives after the drain.
func finish(s exitSteps) {
	var unfinished []string
	drained := make(chan bool, 1)
	go func() { drained <- s.drain() }()
	select {
	case ok := <-drained:
		if !ok {
			unfinished = append(unfinished, "reading the hook events")
		}
	case <-time.After(s.hookWait):
		unfinished = append(unfinished, "reading the hook events")
	case <-s.hurry:
		unfinished = append(unfinished, "reading the hook events (asked to stop again)")
	}
	s.stop()
	select {
	case <-s.ran:
	case <-time.After(s.serviceWait):
		unfinished = append(unfinished, "stopping the services")
	case <-s.hurry:
		unfinished = append(unfinished, "stopping the services (asked to stop again)")
	}
	// Run flushes too once its services end; flushing again is a no-op.
	// The flush waits for a write that a full or slow disk can hold up.
	flushed := make(chan struct{})
	go func() {
		s.flush()
		close(flushed)
	}()
	select {
	case <-flushed:
	case <-time.After(s.flushWait):
		unfinished = append(unfinished, "writing the open approvals")
	}
	if len(unfinished) > 0 {
		s.logf("daemon: exiting before %s finished", strings.Join(unfinished, ", "))
	}
}
