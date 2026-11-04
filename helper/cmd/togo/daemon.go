package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("daemon", func(ctx context.Context, args []string, _ map[string]string, _ io.Reader, _, stderr io.Writer) int {
		return runDaemon(ctx, args, stderr)
	})
	addUsage("  daemon                                        run the helper daemon in the foreground")
}

// runDaemon serves the unix socket until SIGINT or SIGTERM. Diagnostics go
// to stderr, which launchd, systemd and serve's detached start all point at
// the daemon log.
func runDaemon(ctx context.Context, args []string, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, fmt.Errorf("daemon takes no arguments"))
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
	err = daemon.Run(ctx, daemon.Config{
		Home: home,
		Build: func(ctx context.Context) (rpc.Handler, error) {
			h, err := newAPI(ctx, home)
			if err != nil {
				return nil, err
			}
			c, err := daemon.Compose(h, daemon.ComposeOptions{Home: home, TogoPath: exe, Logf: logger.Printf})
			if err != nil {
				return nil, err
			}
			go c.Run(ctx)
			return c.Handler(), nil
		},
		Info: info,
		Logf: logger.Printf,
	})
	// Exit 0 so launchd (KeepAlive unless SuccessfulExit) and systemd
	// (Restart=on-failure) do not keep relaunching a daemon that lost to
	// one serve already started.
	if errors.Is(err, daemon.ErrRunning) {
		fmt.Fprintln(stderr, "togo: a daemon is already running for this TOGO_HOME")
		return exitOK
	}
	if err != nil {
		return fail(stderr, err)
	}
	return exitOK
}
