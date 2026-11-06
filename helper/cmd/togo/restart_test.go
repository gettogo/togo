package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/sockd"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/version"
)

// setVersion makes this binary report v until the test ends.
func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func waitDone(t *testing.T, f *daemontest.Fake) {
	t.Helper()
	select {
	case err := <-f.Done:
		if !errors.Is(err, daemon.ErrShutdown) {
			t.Fatalf("daemon stopped with %v, want a shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop once idle")
	}
}

func TestServeRestartsStaleDaemonWhenIdle(t *testing.T) {
	home := shortHome(t)
	setVersion(t, "v1.0.1")
	var open atomic.Int32
	open.Store(1)
	fake := daemontest.StartFake(t, home, "v1.0.0", func() int { return int(open.Load()) })

	ping := `{"jsonrpc":"2.0","id":2,"method":"ping"}`
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest, ping), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	// The session went to the old daemon, which still answers.
	if _, err := rpc.ReadFrame(&out); err != nil {
		t.Fatalf("hello reply: %v", err)
	}
	if reply, err := rpc.ReadFrame(&out); err != nil || !strings.Contains(string(reply), `"pong"`) {
		t.Fatalf("ping reply = %s, %v; want the old daemon's answer", reply, err)
	}
	if n := fake.D.Requests(); n != 1 {
		t.Errorf("shutdown requests = %d, want 1", n)
	}
	select {
	case err := <-fake.Done:
		t.Fatalf("daemon stopped (%v) with an approval pending", err)
	default:
	}
	open.Store(0)
	waitDone(t, fake)
}

// A daemon from a newer install is left running: restarting it would bring
// the same version back on every connection.
func TestServeLeavesNewerDaemon(t *testing.T) {
	home := shortHome(t)
	setVersion(t, "v1.0.0")
	fake := daemontest.StartFake(t, home, "v1.0.1", nil)
	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"serve", "--stdio", "--log"}, nil, frames(t, helloRequest), &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if n := fake.D.Requests(); n != 0 {
		t.Errorf("shutdown requests = %d, want none", n)
	}
	logged, _ := os.ReadFile(filepath.Join(home, "log", "serve.log"))
	if !strings.Contains(string(logged), "run togo update") {
		t.Errorf("serve.log = %q, want a hint to update", logged)
	}
}

func TestServeIgnoresDevVersions(t *testing.T) {
	for _, tc := range []struct{ serve, daemon string }{
		{"dev", "v1.0.0"},
		{"v1.0.1", "dev"},
		{"v1.0.1", "v1.0.1"},
	} {
		t.Run(tc.serve+"/"+tc.daemon, func(t *testing.T) {
			home := shortHome(t)
			setVersion(t, tc.serve)
			fake := daemontest.StartFake(t, home, tc.daemon, nil)
			var out, errOut bytes.Buffer
			if code := runContext(context.Background(), []string{"serve", "--stdio"}, nil, frames(t, helloRequest), &out, &errOut); code != 0 {
				t.Fatalf("code=%d stderr=%q", code, errOut.String())
			}
			if n := fake.D.Requests(); n != 0 {
				t.Errorf("shutdown requests = %d, want none", n)
			}
		})
	}
}

func TestDaemonRestartWhenIdleWaitsForApproval(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	var open atomic.Int32
	open.Store(1)
	fake := daemontest.StartFake(t, home, "v1.0.0", func() int { return int(open.Load()) })
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"daemon", "restart", "--when-idle"}, nil, nil, &out, &errOut)
	if code != 0 || out.String() != "Restart waits for 1 pending approval\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	open.Store(0)
	waitDone(t, fake)
}

func TestDaemonRestartWaitsBoundedForApproval(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	old := restartPendingWait
	restartPendingWait = 300 * time.Millisecond
	t.Cleanup(func() { restartPendingWait = old })
	daemontest.StartFake(t, home, "v1.0.0", func() int { return 2 })
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"daemon", "restart"}, nil, nil, &out, &errOut)
	want := "Restart waits for 2 pending approvals\nStill waiting; the daemon restarts once they are answered\n"
	if code != 0 || out.String() != want {
		t.Fatalf("code=%d stdout=%q stderr=%q, want %q", code, out.String(), errOut.String(), want)
	}
}

// callDaemon greets the daemon on home and makes one call.
func callDaemon(t *testing.T, home, method, params string) json.RawMessage {
	t.Helper()
	c, err := sockd.Dial(context.Background(), daemon.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	for _, f := range []string{helloRequest, `{"jsonrpc":"2.0","id":2,"method":"` + method + `","params":` + params + `}`} {
		if err := rpc.WriteFrame(c, []byte(f)); err != nil {
			t.Fatal(err)
		}
	}
	for {
		payload, err := rpc.ReadFrame(c)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(payload, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != 2 {
			continue
		}
		if reply.Error != nil {
			t.Fatalf("%s: %s", method, reply.Error)
		}
		return reply.Result
	}
}

func TestRestartKeepsTmuxSessions(t *testing.T) {
	tmpdir, err := os.MkdirTemp("/tmp", "togo-cli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpdir) })
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", tmpdir)
	tm := tmuxtest.New(t)
	home := shortHome(t)
	t.Setenv("TOGO_TMUX_LABEL", tm.Socket[1])
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")

	// Process starts crawl on a loaded machine; every wait here is for a condition, so
	// the ceilings only need to be far above any plausible start time.
	oldStart, oldExit := restartStartWait, restartExitWait
	restartStartWait, restartExitWait = 60*time.Second, 60*time.Second
	t.Cleanup(func() { restartStartWait, restartExitWait = oldStart, oldExit })

	old := exec.Command(togoBin, "daemon")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- old.Wait() }()
	t.Cleanup(func() {
		old.Process.Kill()
		<-exited
	})
	deadline := time.Now().Add(60 * time.Second)
	for !daemonRunning(home) {
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	created := callDaemon(t, home, "session.create", `{"agent":"shell","cwd":"/tmp"}`)
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &sess); err != nil || sess.ID == "" {
		t.Fatalf("session.create = %s, %v", created, err)
	}

	useBuiltDaemon(t, home)
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"daemon", "restart"}, nil, nil, &out, &errOut)
	if code != 0 || out.String() != "Restarted\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	select {
	case err := <-exited:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != daemon.ExitUpgrade {
			t.Fatalf("old daemon exited with %v, want status %d", err, daemon.ExitUpgrade)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("old daemon did not exit")
	}
	exited <- nil // for the cleanup

	if err := exec.Command(tm.Bin, append(tm.Socket, "has-session", "-t", "="+sess.ID)...).Run(); err != nil {
		t.Fatalf("session %s did not survive the restart: %v", sess.ID, err)
	}
	listed := callDaemon(t, home, "session.list", `{}`)
	if !strings.Contains(string(listed), sess.ID) {
		t.Errorf("new daemon lists %s, want %s", listed, sess.ID)
	}
}
