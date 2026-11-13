package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestDaemonRestartCancellationDoesNotReportSuccess(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	fake := daemontest.StartFake(t, home, "v1.0.4", func() int { return 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for fake.D.Requests() == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
	}()
	var out bytes.Buffer
	err := restartDaemon(ctx, home, wire.ShutdownRestart, false, &out)
	if !errors.Is(err, context.Canceled) || strings.Contains(out.String(), "Restarted") {
		t.Fatalf("error=%v stdout=%q", err, out.String())
	}
}

func TestDaemonRestartReportsShutdownTimeout(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	daemontest.StartFake(t, home, "v1.0.4", nil)
	old := restartExitWait
	restartExitWait = time.Millisecond
	t.Cleanup(func() { restartExitWait = old })
	var out bytes.Buffer
	err := restartDaemon(context.Background(), home, wire.ShutdownRestart, false, &out)
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(out.String(), "Restarted") {
		t.Fatalf("error=%v stdout=%q", err, out.String())
	}
}

func TestDaemonRestartReportsReplacementFailure(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	daemontest.StartFake(t, home, "v1.0.4", nil)
	failed := errors.New("replacement could not start")
	old := startDaemon
	startDaemon = func(string) error { return failed }
	t.Cleanup(func() { startDaemon = old })
	var out bytes.Buffer
	err := restartDaemon(context.Background(), home, wire.ShutdownRestart, false, &out)
	if !errors.Is(err, failed) || strings.Contains(out.String(), "Restarted") {
		t.Fatalf("error=%v stdout=%q", err, out.String())
	}
}
