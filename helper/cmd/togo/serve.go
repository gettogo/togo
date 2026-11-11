package main

import (
	"context"
	"fmt"
	"github.com/gettogo/togo/helper/internal/logfile"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/update"
	"github.com/gettogo/togo/helper/internal/version"
	"github.com/gettogo/togo/helper/internal/wire"
)

// capabilities are what the in-process server offers. It has no event hub,
// so it never claims events, approvals or push.
var capabilities = []string{"session.v1", "agents.v1", "fs.v1", "capture.v1"}

// daemonCapabilities are what `togo daemon` offers.
var daemonCapabilities = daemon.Capabilities

// startDaemon starts a daemon for home. Tests replace it so they never start
// a real one by accident.
var startDaemon = func(home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return startDaemonVia(ctx, home, managedService(ctx), detachDaemon)
}

// detachDaemon launches a detached daemon, outside any service manager.
func detachDaemon(home string) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	_, err = daemon.Start(exe, home)
	return err
}

// managedService returns the user service when the service manager owns the
// daemon, and nil when it does not (no service installed or loaded).
func managedService(ctx context.Context) *daemon.Service {
	svc, err := userService()
	if err != nil || !svc.Managed(ctx) {
		return nil
	}
	return &svc
}

// startDaemonVia picks how a missing daemon is started. Under a service
// manager it asks the manager, so the daemon stays supervised: a detached
// daemon would take the lock, the manager's own restart would lose the race
// and exit 0, and the manager would then never restart the daemon again.
// Without a service, or when the manager refuses, it falls back to detach.
func startDaemonVia(ctx context.Context, home string, svc *daemon.Service, detach func(home string) error) error {
	if svc != nil {
		if err := svc.Start(ctx); err == nil {
			return nil
		}
	}
	return detach(home)
}

// runServe answers protocol requests on stdin and stdout until stdin ends.
// It proxies to the daemon, starting one when none answers, and serves in
// process when that fails. stdout carries frames only, so diagnostics go to
// the log file when --log is given and are dropped otherwise; stderr is for
// failures that stop the helper.
func runServe(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("serve")
	stdio := fs.Bool("stdio", false, "serve over stdin and stdout")
	logToFile := fs.Bool("log", false, "write diagnostics to $TOGO_HOME/log/serve.log")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return usageError(stderr, fmt.Errorf("serve takes no arguments"))
	}
	if !*stdio {
		return usageError(stderr, fmt.Errorf("serve needs --stdio"))
	}

	home, err := state.Home()
	if err != nil {
		return fail(stderr, err)
	}
	logf := func(string, ...any) {}
	if *logToFile {
		logger, closeLog, err := openServeLog(home)
		if err != nil {
			return fail(stderr, err)
		}
		defer closeLog()
		logf = logger.Printf
	}

	socket := daemon.SocketPath(home)
	conn, daemonVersion, err := daemon.ConnectVersion(ctx, socket, func() error { return startDaemon(home) }, daemon.StartWait)
	if err == nil {
		installed := version.Current()
		switch {
		case stale(daemonVersion, installed):
			// The daemon predates the installed binary. It exits once
			// nothing is pending; this session stays with it until then,
			// and the next serve reaches the new one.
			res, err := daemon.RequestShutdown(ctx, socket, wire.ShutdownParams{Reason: wire.ShutdownUpgrade, When: wire.ShutdownIdle})
			if err != nil {
				logf("serve: daemon %s is older than this binary (%s); restart request failed: %v", daemonVersion, installed, err)
			} else {
				logf("serve: daemon %s is older than this binary (%s); it restarts when idle (%d pending)", daemonVersion, installed, res.Pending)
			}
		case stale(installed, daemonVersion):
			// Another, newer install runs the daemon; restarting it would
			// bring back the same version on every connection.
			logf("serve: this binary (%s) is older than the daemon (%s); run togo update", installed, daemonVersion)
		}
		if err := daemon.Pump(ctx, conn, stdin, stdout); err != nil {
			logf("serve: %v", err)
			return fail(stderr, err)
		}
		return exitOK
	}
	logf("serve: daemon unavailable, serving in process without events: %v", err)
	return serveInProcess(ctx, home, logf, stdin, stdout, stderr)
}

// stale reports whether a daemon reporting running is older than the
// binary reporting installed. Only release versions compare: development
// builds never trigger a restart, and a newer daemon is left alone.
func stale(running, installed string) bool {
	return update.ValidVersion(running) && update.ValidVersion(installed) && update.Compare(running, installed) < 0
}

func serveInProcess(ctx context.Context, home string, logf func(string, ...any), stdin io.Reader, stdout, stderr io.Writer) int {
	h, err := newAPI(ctx, home)
	if err != nil {
		return fail(stderr, err)
	}
	h.Logf = logf
	info, err := helperInfo(capabilities)
	if err != nil {
		return fail(stderr, err)
	}

	// A blocking read on stdin cannot be interrupted by closing it, so Serve
	// runs on its own goroutine and a signal ends the command without
	// waiting for it. The process exits right after, which ends the read.
	served := make(chan error, 1)
	go func() { served <- rpc.Serve(ctx, stdin, stdout, h, info) }()
	select {
	case <-ctx.Done():
		return exitOK
	case err := <-served:
		if err != nil && ctx.Err() == nil {
			logf("serve: %v", err)
			return fail(stderr, err)
		}
		return exitOK
	}
}

func newAPI(ctx context.Context, home string) (*api.API, error) {
	svc, err := session.NewService(ctx, home)
	if err != nil {
		return nil, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &api.API{Sessions: svc, Recent: svc.Recent, Home: userHome}, nil
}

func helperInfo(caps []string) (rpc.HelperInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return rpc.HelperInfo{}, err
	}
	return rpc.HelperInfo{
		Version:      version.Current(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Hostname:     hostname,
		Capabilities: caps,
	}, nil
}

// executable returns the resolved absolute path of the running binary, the
// path a detached daemon or a service definition must run.
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate togo binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("locate togo binary: %w", err)
	}
	return exe, nil
}

// openServeLog opens $TOGO_HOME/log/serve.log for appending. Both the
// directory and the file are private to the user, since logs can name paths.
func openServeLog(home string) (*log.Logger, func(), error) {
	dir := filepath.Join(home, "log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("log directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("log directory: %w", err)
	}
	f, err := logfile.Open(filepath.Join(dir, "serve.log"), logfile.MaxSize)
	if err != nil {
		return nil, nil, fmt.Errorf("log file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("log file: %w", err)
	}
	return log.New(f, "", log.LstdFlags), func() { f.Close() }, nil
}
