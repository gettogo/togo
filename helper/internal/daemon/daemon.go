// Package daemon runs the long-lived helper process that owns the unix
// socket, proxies `togo serve --stdio` to it, and installs it as a per-user
// launchd or systemd service.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
)

// Config is what Run needs to serve one TOGO_HOME.
type Config struct {
	// Home is TOGO_HOME; the socket and lock live in its run directory.
	Home string
	// Build makes the handler. It runs after the socket is bound, because
	// building can take seconds (it asks the login shell for PATH) and a
	// serve that just started the daemon gives up if the socket is not
	// there in time.
	Build func(ctx context.Context) (rpc.Handler, error)
	Info  rpc.HelperInfo
	// Logf receives diagnostics. Nil drops them.
	Logf func(format string, args ...any)
}

// SocketPath returns the daemon socket for a TOGO_HOME.
func SocketPath(home string) string {
	return filepath.Join(home, "run", "togo.sock")
}

// LockPath returns the file a running daemon holds locked.
func LockPath(home string) string {
	return filepath.Join(home, "run", "daemon.lock")
}

// PIDPath returns the file holding the pid of the daemon that holds the
// lock. It is meaningful only while the lock is held.
func PIDPath(home string) string {
	return filepath.Join(home, "run", "daemon.pid")
}

// ErrRunning is returned by Run when another daemon holds the lock for the
// same TOGO_HOME.
var ErrRunning = errors.New("daemon already running")

// Run serves cfg.Handler on the TOGO_HOME socket until ctx is cancelled,
// then stops accepting, closes open connections and removes the socket.
// The lock is taken before the socket is bound so a second daemon fails on
// the lock and never touches the first one's socket.
func Run(ctx context.Context, cfg Config) error {
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	release, err := sockd.Lock(LockPath(cfg.Home))
	if errors.Is(err, sockd.ErrLocked) {
		return ErrRunning
	}
	if err != nil {
		return fmt.Errorf("daemon lock: %w", err)
	}
	defer release()
	pidFile := PIDPath(cfg.Home)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return fmt.Errorf("daemon pid file: %w", err)
	}
	defer os.Remove(pidFile)

	ln, err := sockd.Listen(SocketPath(cfg.Home))
	if err != nil {
		return fmt.Errorf("daemon listen: %w", err)
	}
	logf("daemon: listening")
	handler := newPendingHandler()
	built := make(chan error, 1)
	go func() {
		h, err := cfg.Build(ctx)
		if err == nil {
			handler.set(h)
		} else {
			handler.fail()
		}
		built <- err
	}()

	var (
		mu     sync.Mutex
		conns  = map[net.Conn]struct{}{}
		closed bool
		wg     sync.WaitGroup
	)
	shutdown := func() {
		ln.Close()
		mu.Lock()
		closed = true
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
	}
	stop := context.AfterFunc(ctx, shutdown)
	defer stop()
	var buildErr error
	go func() {
		if err := <-built; err != nil && ctx.Err() == nil {
			mu.Lock()
			buildErr = fmt.Errorf("daemon build: %w", err)
			mu.Unlock()
			shutdown()
		}
	}()

	var acceptErr error
	for {
		c, err := ln.Accept()
		if err != nil {
			mu.Lock()
			wasClosed := closed
			mu.Unlock()
			if !wasClosed && ctx.Err() == nil {
				acceptErr = fmt.Errorf("daemon accept: %w", err)
				ln.Close()
			}
			break
		}
		mu.Lock()
		if closed {
			mu.Unlock()
			c.Close()
			break
		}
		conns[c] = struct{}{}
		mu.Unlock()
		wg.Go(func() {
			defer func() {
				mu.Lock()
				delete(conns, c)
				mu.Unlock()
				c.Close()
			}()
			if err := rpc.Serve(ctx, c, c, handler, cfg.Info); err != nil && ctx.Err() == nil {
				logf("daemon: connection: %v", err)
			}
		})
	}
	wg.Wait()
	logf("daemon: stopped")
	mu.Lock()
	defer mu.Unlock()
	if buildErr != nil {
		return buildErr
	}
	return acceptErr
}

// pendingHandler holds calls until the real handler is built. hello is
// answered by rpc.Serve without a handler, so clients connect and greet
// at once and only their first method call waits. A failed build fails
// the waiting calls; otherwise their connections never finish and Run
// never returns.
type pendingHandler struct {
	ready  chan struct{}
	failed chan struct{}
	h      rpc.Handler
}

func newPendingHandler() *pendingHandler {
	return &pendingHandler{ready: make(chan struct{}), failed: make(chan struct{})}
}

func (p *pendingHandler) fail() { close(p.failed) }

func (p *pendingHandler) set(h rpc.Handler) {
	p.h = h
	close(p.ready)
}

func (p *pendingHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.Error) {
	select {
	case <-p.ready:
		return p.h.Handle(ctx, method, params)
	case <-p.failed:
		return nil, rpc.NewError(rpc.CodeInternal, nil)
	case <-ctx.Done():
		return nil, rpc.NewError(rpc.CodeInternal, nil)
	}
}
