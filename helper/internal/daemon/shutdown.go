package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ExitUpgrade is the daemon's exit status after local.shutdown. It is not
// zero, so launchd (KeepAlive unless SuccessfulExit) and systemd
// (Restart=on-failure) start the service again, now from the installed
// binary.
const ExitUpgrade = 75

// ErrShutdown is returned by Run when the daemon stopped for local.shutdown.
var ErrShutdown = errors.New("daemon shut down for restart")

// pollPending is how often a deferred shutdown counts what is pending, and
// settle is how long the count must stay at zero before the daemon exits.
// The wait lets the answer to the last approval or ask reach its caller.
// Variables so tests can shorten them.
var (
	pollPending = 100 * time.Millisecond
	settle      = 200 * time.Millisecond
)

// Daemon tracks what a restart must wait for: approvals still open and
// local.ask calls still running. Run serves local.shutdown through it.
type Daemon struct {
	mu        sync.Mutex
	pending   func() int
	asks      int
	requested bool
	requests  int
	exit      chan struct{}
	exitOnce  sync.Once
	stopped   chan struct{}
	stopOnce  sync.Once
	logf      func(format string, args ...any)
}

// New returns a Daemon with nothing pending.
func New() *Daemon {
	return &Daemon{exit: make(chan struct{}), stopped: make(chan struct{}), logf: func(string, ...any) {}}
}

// stop ends a deferred wait once Run has returned for another reason.
func (d *Daemon) stop() { d.stopOnce.Do(func() { close(d.stopped) }) }

// SetPending installs the count of open approvals, not counting the ones
// that local.ask calls opened, which the Daemon counts itself.
func (d *Daemon) SetPending(count func() int) {
	d.mu.Lock()
	d.pending = count
	d.mu.Unlock()
}

// Pending returns how many approvals and asks a restart waits for.
func (d *Daemon) Pending() int {
	d.mu.Lock()
	count, asks := d.pending, d.asks
	d.mu.Unlock()
	n := asks
	if count != nil {
		n += count()
	}
	return n
}

// Requests returns how many shutdown requests the daemon has received.
func (d *Daemon) Requests() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests
}

// Exit is closed when the daemon should stop for a restart.
func (d *Daemon) Exit() <-chan struct{} { return d.exit }

// Shutdown asks the daemon to exit. With wire.ShutdownIdle it exits as
// soon as nothing is pending, which may be now. Repeated requests are
// answered with the current count and start nothing new.
func (d *Daemon) Shutdown(when string) wire.ShutdownResult {
	_ = when // ShutdownIdle is the only mode; the handler rejects others.
	d.mu.Lock()
	d.requests++
	first := !d.requested
	d.requested = true
	d.mu.Unlock()
	n := d.Pending()
	if first {
		go d.waitIdle()
	}
	return wire.ShutdownResult{Deferred: n > 0, Pending: n}
}

// waitIdle closes Exit once the pending count has stayed at zero for
// settle.
func (d *Daemon) waitIdle() {
	var idleSince time.Time
	for {
		if d.Pending() > 0 {
			idleSince = time.Time{}
		} else {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= settle {
				d.exitOnce.Do(func() { close(d.exit) })
				return
			}
		}
		select {
		case <-d.stopped:
			return
		case <-time.After(min(pollPending, settle)):
		}
	}
}

func (d *Daemon) askStarted() {
	d.mu.Lock()
	d.asks++
	d.mu.Unlock()
}

func (d *Daemon) askDone() {
	d.mu.Lock()
	d.asks--
	d.mu.Unlock()
}

// wrap serves local.shutdown and counts local.ask calls while they run,
// passing every other call to next.
func (d *Daemon) wrap(next rpc.Handler) rpc.Handler {
	return &shutdownHandler{d: d, next: next}
}

type shutdownHandler struct {
	d    *Daemon
	next rpc.Handler
}

func (h *shutdownHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.Error) {
	switch method {
	case wire.MethodLocalShutdown:
		var p wire.ShutdownParams
		if err := api.Decode(params, &p); err != nil || p.When != wire.ShutdownIdle {
			return nil, rpc.InvalidParams("when must be idle")
		}
		res := h.d.Shutdown(p.When)
		h.d.logf("daemon: shutdown requested (%s), %d pending", p.Reason, res.Pending)
		return res, nil
	case wire.MethodLocalAsk:
		h.d.askStarted()
		defer h.d.askDone()
	}
	return h.next.Handle(ctx, method, params)
}
