package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
)

// togoBin is a togo binary built for tests that need a real daemon process.
var togoBin string

func TestMain(m *testing.M) {
	// A test that reaches the real start path would exec the test binary
	// as a daemon; tests that want a daemon opt in.
	startDaemon = func(string) error { return errors.New("daemon start disabled in tests") }
	serviceRunner = func(context.Context, []string) error { return errors.New("service manager disabled in tests") }

	dir, err := os.MkdirTemp("", "togobin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	togoBin = filepath.Join(dir, "togo")
	build := exec.Command("go", "build", "-o", togoBin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build togo:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// shortHome points TOGO_HOME at a directory short enough for a unix socket
// path and the tmux label at a private one the daemon would use.
func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TOGO_HOME", dir)
	t.Setenv("TOGO_TMUX_LABEL", fmt.Sprintf("togo-test-daemon-%d", os.Getpid()))
	return dir
}

func helloCapabilities(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()
	payload, err := rpc.ReadFrame(out)
	if err != nil {
		t.Fatalf("reading reply: %v", err)
	}
	var reply struct {
		Result struct {
			Capabilities []string `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("reply %s: %v", payload, err)
	}
	return reply.Result.Capabilities
}

// useBuiltDaemon makes serve start the built binary and stops that daemon
// when the test ends.
func useBuiltDaemon(t *testing.T, home string) *bool {
	t.Helper()
	started := false
	startDaemon = func(h string) error {
		_, err := daemon.Start(togoBin, h)
		started = started || err == nil
		return err
	}
	t.Cleanup(func() {
		startDaemon = func(string) error { return errors.New("daemon start disabled in tests") }
		daemontest.Stop(t, home)
		if _, err := os.Lstat(daemon.SocketPath(home)); !os.IsNotExist(err) {
			t.Error("daemon did not remove its socket after SIGTERM")
		}
	})
	return &started
}

func checkServedByDaemon(t *testing.T, home string, out *bytes.Buffer) {
	t.Helper()
	if caps := helloCapabilities(t, out); !slices.Equal(caps, daemonCapabilities) {
		t.Errorf("capabilities = %q, want the daemon's %q", caps, daemonCapabilities)
	}
	c, err := sockd.Dial(context.Background(), daemon.SocketPath(home))
	if err != nil {
		t.Fatalf("daemon not serving after serve exited: %v", err)
	}
	c.Close()
	logged, _ := os.ReadFile(filepath.Join(home, "log", "serve.log"))
	if strings.Contains(string(logged), "in process") {
		t.Errorf("serve fell back although the daemon started: %s", logged)
	}
}

func TestServeStartsDaemonWhenAbsent(t *testing.T) {
	home := shortHome(t)
	started := useBuiltDaemon(t, home)
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if !*started {
		t.Fatal("serve did not start a daemon")
	}
	checkServedByDaemon(t, home, &out)
}

// A login shell that hangs past its lookup timeout used to delay the bind
// past serve's start window, so serve fell back without events.
func TestServeUsesDaemonDespiteSlowLoginShell(t *testing.T) {
	home := shortHome(t)
	shell := filepath.Join(home, "slowsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	useBuiltDaemon(t, home)
	listDir := `{"jsonrpc":"2.0","id":2,"method":"fs.listDir","params":{"path":"/"}}`
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest, listDir), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	checkServedByDaemon(t, home, &out)
	payload, err := rpc.ReadFrame(&out)
	if err != nil || !strings.Contains(string(payload), `"result"`) {
		t.Errorf("fs.listDir reply = %s, %v; want a result once the daemon is built", payload, err)
	}
}

func TestServeFallsBackWhenDaemonDoesNotAnswer(t *testing.T) {
	home := shortHome(t)
	ln, err := sockd.Listen(daemon.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if caps := helloCapabilities(t, &out); !slices.Equal(caps, capabilities) {
		t.Errorf("capabilities = %q, want the in-process %q", caps, capabilities)
	}
	logged, _ := os.ReadFile(filepath.Join(home, "log", "serve.log"))
	if !strings.Contains(string(logged), "did not answer hello") {
		t.Errorf("serve.log = %q, want the unresponsive daemon named", logged)
	}
}

func TestServeFallsBackInProcess(t *testing.T) {
	home := shortHome(t)
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	caps := helloCapabilities(t, &out)
	if len(caps) == 0 || slices.Contains(caps, "events.v1") || slices.Contains(caps, "approvals.v1") || slices.Contains(caps, "push.v1") {
		t.Errorf("fallback capabilities = %q, want none of events, approvals, push", caps)
	}
	logged, err := os.ReadFile(filepath.Join(home, "log", "serve.log"))
	if err != nil || !strings.Contains(string(logged), "daemon start disabled in tests") {
		t.Errorf("serve.log = %q, %v; want the reason for the fallback", logged, err)
	}
}

func TestDaemonCommandExitsCleanlyWhenOneRuns(t *testing.T) {
	home := shortHome(t)
	release, err := sockd.Lock(filepath.Join(home, "run", "daemon.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var errOut bytes.Buffer
	code := runContext(context.Background(), []string{"daemon"}, nil, nil, &bytes.Buffer{}, &errOut)
	if code != exitOK || !strings.Contains(errOut.String(), "already running") {
		t.Errorf("code=%d stderr=%q, want exit 0 naming the running daemon", code, errOut.String())
	}
}

func TestServiceRejectsUnknownAction(t *testing.T) {
	var errOut bytes.Buffer
	code := runContext(context.Background(), []string{"service", "restart"}, nil, nil, &bytes.Buffer{}, &errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "restart") {
		t.Errorf("code=%d stderr=%q, want a usage error naming the action", code, errOut.String())
	}
}
