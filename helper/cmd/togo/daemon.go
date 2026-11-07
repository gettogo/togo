package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/awake"
	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	register("daemon", func(ctx context.Context, args []string, _ map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		if len(args) > 0 && args[0] == "restart" {
			return runDaemonRestart(ctx, args[1:], stdout, stderr)
		}
		return runDaemon(ctx, args, stderr)
	})
	addUsage("  daemon                                        run the helper daemon in the foreground")
	addUsage("  daemon restart [--when-idle]                  restart the daemon once no approval is pending")
}

// awakeTestEnv, when set, makes the daemon report keeping awake as
// unsupported and touch no power state. The end-to-end tests set it.
const awakeTestEnv = "TOGO_TEST_AWAKE_OFF"

// hostAwake returns the OS side of keeping this machine awake.
var hostAwake = func() awake.Config {
	if os.Getenv(awakeTestEnv) != "" {
		return awake.Config{}
	}
	return awake.ForHost(runtime.GOOS, os.Getpid())
}

// runDaemon serves the unix socket until SIGINT or SIGTERM, or until
// local.shutdown finds nothing pending, when it exits with
// daemon.ExitUpgrade. Diagnostics go to stderr, which launchd, systemd and
// serve's detached start all point at the daemon log.
func runDaemon(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, fmt.Errorf("daemon takes no arguments other than restart"))
	}
	home, err := state.Home()
	if err != nil {
		return fail(stderr, err)
	}
	logger := log.New(stderr, "", log.LstdFlags)
	info, err := helperInfo(daemonCapabilities)
	if err != nil {
		return fail(stderr, err)
	}
	info.Concurrent = daemon.Concurrent
	exe, err := executable()
	if err != nil {
		return fail(stderr, err)
	}
	// Taps name the path that survives upgrades, so a tap tmux restarts
	// after brew removed this version's directory still finds a binary.
	tapPath := daemon.StableExecutable(exe)
	d := daemon.New()
	// The daemon exits as soon as Run returns, so the awake service is
	// shut down here rather than left to its own goroutine: the hold and
	// any disablesleep must be released on SIGINT and SIGTERM.
	var (
		mu    sync.Mutex
		built *daemon.Components
	)
	hostAwake := hostAwake()
	err = daemon.Run(ctx, daemon.Config{
		Home:   home,
		Daemon: d,
		Build: func(ctx context.Context) (rpc.Handler, error) {
			h, err := newAPI(ctx, home)
			if err != nil {
				return nil, err
			}
			state.PersistLoginShell(home)
			state.WarmLoginPath(ctx, logger.Printf)
			c, err := daemon.Compose(h, daemon.ComposeOptions{Home: home, TogoPath: tapPath, Awake: &hostAwake, Logf: logger.Printf})
			if err != nil {
				return nil, err
			}
			d.SetPending(c.PendingApprovals)
			mu.Lock()
			built = c
			mu.Unlock()
			go c.Run(ctx)
			return c.Handler(), nil
		},
		Info: info,
		Logf: logger.Printf,
	})
	mu.Lock()
	if built != nil {
		built.Awake.Shutdown(context.WithoutCancel(ctx))
	}
	mu.Unlock()
	// Exit 0 so launchd (KeepAlive unless SuccessfulExit) and systemd
	// (Restart=on-failure) do not keep relaunching a daemon that lost to
	// one serve already started.
	if errors.Is(err, daemon.ErrRunning) {
		fmt.Fprintln(stderr, "togo: a daemon is already running for this TOGO_HOME")
		return exitOK
	}
	if errors.Is(err, daemon.ErrShutdown) {
		return daemon.ExitUpgrade
	}
	if err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// Restart waits: how long a restart waits for pending approvals when it
// was not told to leave it to the daemon, how long for the daemon to exit
// once idle, and how long for the service manager to start it again.
// Variables so tests can shorten them.
var (
	restartPendingWait = 10 * time.Minute
	restartExitWait    = 10 * time.Second
	restartServiceWait = 15 * time.Second
	restartStartWait   = daemon.StartWait
)

func runDaemonRestart(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("daemon restart")
	whenIdle := fs.Bool("when-idle", false, "return at once and let the daemon restart when nothing is pending")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return usageError(stderr, fmt.Errorf("daemon restart takes no arguments"))
	}
	home, err := state.Home()
	if err != nil {
		return fail(stderr, err)
	}
	if err := restartDaemon(ctx, home, wire.ShutdownRestart, *whenIdle, stdout); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// restartDaemon asks the daemon for home to exit once no approval or ask is
// pending, then makes sure a daemon runs again. With whenIdle it returns as
// soon as the daemon has agreed to wait; otherwise it waits up to
// restartPendingWait. No daemon running is not an error.
func restartDaemon(ctx context.Context, home, reason string, whenIdle bool, stdout io.Writer) error {
	socket := daemon.SocketPath(home)
	if !daemonRunning(home) {
		fmt.Fprintln(stdout, "No daemon is running; the next connection starts one")
		return nil
	}
	res, err := daemon.RequestShutdown(ctx, socket, wire.ShutdownParams{Reason: reason, When: wire.ShutdownIdle})
	if err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	wait := restartExitWait
	if res.Deferred {
		fmt.Fprintf(stdout, "Restart waits for %s\n", pendingApprovals(res.Pending))
		if whenIdle {
			return nil
		}
		wait = restartPendingWait
	}
	if !waitStopped(ctx, home, wait) {
		if res.Deferred {
			fmt.Fprintln(stdout, "Still waiting; the daemon restarts once they are answered")
			return nil
		}
		return errors.New("restart: the daemon did not stop")
	}
	if err := ensureDaemon(ctx, home); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	fmt.Fprintln(stdout, "Restarted")
	return nil
}

func pendingApprovals(n int) string {
	if n == 1 {
		return "1 pending approval"
	}
	return fmt.Sprintf("%d pending approvals", n)
}

// daemonRunning reports whether a daemon holds the lock for home.
func daemonRunning(home string) bool {
	release, err := sockd.Lock(daemon.LockPath(home))
	if err == nil {
		release()
		return false
	}
	return errors.Is(err, sockd.ErrLocked)
}

// waitStopped polls until no daemon holds the lock for home.
func waitStopped(ctx context.Context, home string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for daemonRunning(home) {
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
	return true
}

// ensureDaemon waits for the service manager to start the daemon again when
// the service is loaded, and starts one itself otherwise or when the
// service is slow.
func ensureDaemon(ctx context.Context, home string) error {
	socket := daemon.SocketPath(home)
	if svc, err := userService(); err == nil {
		if st, err := svc.Status(ctx); err == nil && st.Installed && st.Loaded {
			if c, err := daemon.Connect(ctx, socket, func() error { return nil }, restartServiceWait); err == nil {
				c.Close()
				return nil
			}
		}
	}
	c, err := daemon.Connect(ctx, socket, func() error { return startDaemon(home) }, restartStartWait)
	if err != nil {
		return err
	}
	c.Close()
	return nil
}
