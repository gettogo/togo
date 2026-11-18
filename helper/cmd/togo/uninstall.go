package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	register("uninstall", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runUninstall(ctx, args, env, stdout, stderr)
	})
	addUsage("  uninstall                                     remove agent hooks and the login service")
}

// runUninstall undoes what setup added outside $TOGO_HOME. It keeps going
// after a failure so one broken config does not leave the service behind.
func runUninstall(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, fmt.Errorf("uninstall takes no arguments"))
	}
	code := exitOK
	if o, err := hookOptions(env, false, false); err != nil {
		code = fail(stderr, err)
	} else if c := printHookResults(hooks.Uninstall(o, append(slices.Clone(hooks.Agents), hooks.Retired...)), stdout, stderr); c != exitOK {
		code = c
	}
	svc, err := userService()
	if err != nil {
		return fail(stderr, err)
	}
	if err := uninstallService(ctx, svc, stderr); err != nil {
		code = fail(stderr, err)
	} else {
		fmt.Fprintln(stdout, "service: uninstalled")
	}
	if home, err := state.Home(); err == nil {
		if err := stopDetachedDaemon(ctx, home, stdout); err != nil {
			code = fail(stderr, err)
		}
		if c := uninstallAwake(ctx, home, stdout, stderr); c != exitOK {
			code = c
		}
		fmt.Fprintf(stdout, "Left %s in place; delete it to remove sessions and keys.\n", home)
	}
	return code
}

// stopDetachedDaemon asks a daemon that is still running after the service
// is gone, such as one serve started, to exit once nothing is pending. The
// service manager stops a managed daemon itself, so this is a no-op then.
func stopDetachedDaemon(ctx context.Context, home string, stdout io.Writer) error {
	if !daemonRunning(home) {
		return nil
	}
	watch, err := daemon.BeginShutdown(ctx, daemon.SocketPath(home), wire.ShutdownParams{Reason: wire.ShutdownRestart, When: wire.ShutdownIdle})
	if err != nil {
		return fmt.Errorf("daemon: could not ask it to stop: %w", err)
	}
	defer watch.Close()
	if watch.Result.Deferred {
		fmt.Fprintf(stdout, "daemon: stops once %s is answered\n", pendingApprovals(watch.Result.Pending))
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, restartExitWait)
	defer cancel()
	if err := watch.Wait(waitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintln(stdout, "daemon: asked to stop; it is still shutting down")
			return nil
		}
		return fmt.Errorf("daemon: waiting for it to stop: %w", err)
	}
	fmt.Fprintln(stdout, "daemon: stopped")
	return nil
}
