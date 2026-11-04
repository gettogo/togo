package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/version"
)

var capabilities = []string{"session.v1", "agents.v1", "fs.v1"}

// runServe answers protocol requests on stdin and stdout until stdin ends.
// stdout carries frames only, so diagnostics go to the log file when --log
// is given and are dropped otherwise; stderr is for failures that stop the
// helper.
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
	svc, err := session.NewService(ctx, home)
	if err != nil {
		return fail(stderr, err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fail(stderr, err)
	}
	h := &api.API{Sessions: svc, Recent: svc.Recent, Home: userHome}
	if *logToFile {
		logger, closeLog, err := openServeLog(home)
		if err != nil {
			return fail(stderr, err)
		}
		defer closeLog()
		h.Logf = logger.Printf
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fail(stderr, err)
	}
	info := rpc.HelperInfo{
		Version:      version.Version,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Hostname:     hostname,
		Capabilities: capabilities,
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
			if h.Logf != nil {
				h.Logf("serve: %v", err)
			}
			return fail(stderr, err)
		}
		return exitOK
	}
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
	f, err := os.OpenFile(filepath.Join(dir, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("log file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("log file: %w", err)
	}
	return log.New(f, "", log.LstdFlags), func() { f.Close() }, nil
}
