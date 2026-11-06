package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// restartEnv is a Claude Code session on a tmux server that outlives the
// daemons composed over it.
type restartEnv struct {
	tm       *tmux.Client
	home     string
	togoHome string
	bin      string
	shell    string
}

func newRestartEnv(t *testing.T) *restartEnv {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &restartEnv{home: home, bin: filepath.Join(home, "bin"), togoHome: t.TempDir()}
	for _, d := range []string{filepath.Join(home, "proj"), e.bin} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Exits 1 on its first run, then waits at its prompt.
	if err := os.WriteFile(filepath.Join(e.bin, "claude"), []byte(lifecycleClaude["session.relaunch"]), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	e.tm = tmuxtest.New(t)
	if p, err := exec.LookPath(e.tm.Bin); err == nil {
		e.tm.Bin = p
	}
	t.Setenv("PATH", e.bin+":/usr/bin:/bin")
	e.shell = fixtureShell(t)
	return e
}

// daemon composes a fresh daemon, as a restart would, and runs its first
// reconcile.
func (e *restartEnv) daemon(t *testing.T) *Components {
	t.Helper()
	recent := &state.Recent{File: filepath.Join(e.togoHome, "recent.json")}
	svc := &session.Service{
		Tmux: e.tm, Recent: recent, Shell: e.shell, Path: e.bin,
		LockFile: filepath.Join(e.togoHome, "run", "bib.lock"),
		Manifest: &session.Manifest{Path: filepath.Join(e.togoHome, "sessions.json")},
		NewID:    func() string { return "s_abcdefghij" },
		Now:      func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
	}
	c, err := Compose(&api.API{Sessions: svc, Recent: recent, Home: e.togoHome}, ComposeOptions{Home: e.togoHome})
	if err != nil {
		t.Fatal(err)
	}
	c.Watch.Reconcile(context.Background())
	return c
}

var helloFrame = json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"t","version":"0"},"capabilities":[]}}`)

// listed returns the session as session.list reports it.
func listed(t *testing.T, c *Components) session.Session {
	t.Helper()
	raw := serve(t, c.Handler(), helloFrame, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.list","params":{}}`))
	var res struct {
		Result struct{ Sessions []session.Session } `json:"result"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || len(res.Result.Sessions) != 1 {
		t.Fatalf("session.list %s", raw)
	}
	return res.Result.Sessions[0]
}

// events records what the hub publishes from now on.
type events struct {
	mu  sync.Mutex
	all []wire.Event
}

func record(c *Components) *events {
	r := &events{}
	c.Hub.OnEvent(func(ev wire.Event) {
		r.mu.Lock()
		r.all = append(r.all, ev)
		r.mu.Unlock()
	})
	return r
}

// active reports a published status that keeps the machine awake and arms
// the will, or a done notification.
func (r *events) active() (wire.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.all {
		switch p := ev.Payload.(type) {
		case wire.SessionEvent:
			if s, ok := p.Session.(session.Session); ok && (s.Status == "working" || s.Status == "needs_you") {
				return ev, true
			}
		case wire.NotifyEvent:
			if p.Level == wire.LevelDone {
				return ev, true
			}
		}
	}
	return wire.Event{}, false
}

// shellOutput sends output from the shell the pane fell back to, then lets
// the idle window pass.
func shellOutput(t *testing.T, c *Components) {
	t.Helper()
	ctx := context.Background()
	if err := c.Watch.Signal(ctx, wire.Signal{SessionID: "s_abcdefghij", Kind: wire.SignalOutput}); err != nil {
		t.Fatal(err)
	}
	c.Watch.SetIdle(time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	c.Watch.Tick(ctx)
}

// waitAgentGone waits for the pane to settle at the shell it fell back to,
// which runs its profile first.
func waitAgentGone(t *testing.T, svc *session.Service, id string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if gone, err := svc.AgentGone(context.Background(), id); err == nil && gone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane never settled at the shell")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func relaunch(t *testing.T, c *Components) json.RawMessage {
	t.Helper()
	return serve(t, c.Handler(), helloFrame, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.relaunch","params":{"id":"s_abcdefghij"}}`))
}

// A daemon restart keeps a failed session failed: the shell it fell back
// to sends no done and holds nothing awake, and Run again still works,
// once.
func TestDaemonRestartKeepsEnded(t *testing.T) {
	e := newRestartEnv(t)
	first := e.daemon(t)
	serve(t, first.Handler(), helloFrame, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.create","params":{"agent":"claude","cwd":"`+e.home+`/proj"}}`))
	reportExit(t, first, "s_abcdefghij", 1)

	c := e.daemon(t)
	if !c.Watch.Ended("s_abcdefghij") {
		t.Fatal("the restarted daemon does not see the session ended")
	}
	if s := listed(t, c); s.Status != wire.StatusFailed || s.Exit == nil || s.Exit.Code == nil || *s.Exit.Code != 1 {
		t.Fatalf("listed after restart %+v, want failed with code 1", s)
	}
	seen := record(c)
	shellOutput(t, c)
	if ev, ok := seen.active(); ok {
		t.Fatalf("shell output after the restart published %+v", ev)
	}

	waitAgentGone(t, c.API.Sessions, "s_abcdefghij")
	raw := relaunch(t, c)
	var res struct {
		Result session.Session `json:"result"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.Result.ID != "s_abcdefghij" || res.Result.Exit != nil {
		t.Fatalf("session.relaunch %s", raw)
	}
	// A second tap before the new run has started is not a second run.
	var conflict struct {
		Error struct {
			Code int
			Data struct{ Reason string }
		} `json:"error"`
	}
	raw = relaunch(t, c)
	if err := json.Unmarshal(raw, &conflict); err != nil || conflict.Error.Code != -32006 || conflict.Error.Data.Reason != wire.ConflictRunning {
		t.Fatalf("second session.relaunch %s, want -32006 running", raw)
	}
	// The relaunch cleared the stored exit too.
	waitAgentRunning(t, c.API.Sessions, "s_abcdefghij")
	if again := e.daemon(t); again.Watch.Ended("s_abcdefghij") {
		t.Fatal("a daemon started after the relaunch sees the session ended")
	}
}

// An exit report lost while the daemon was down leaves no stored exit. A
// daemon that starts within seconds of the pane's start cannot tell an
// agent that is gone from one still starting, so it leaves the session
// alone on both of its start scans.
func TestDaemonStartLeavesANewPaneAlone(t *testing.T) {
	e := newRestartEnv(t)
	first := e.daemon(t)
	serve(t, first.Handler(), helloFrame, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"session.create","params":{"agent":"claude","cwd":"`+e.home+`/proj"}}`))
	reportExit(t, first, "s_abcdefghij", 1)
	if err := e.tm.UnsetOption(context.Background(), "s_abcdefghij", "@togo_exit"); err != nil {
		t.Fatal(err)
	}
	waitAgentGone(t, first.API.Sessions, "s_abcdefghij")

	c := e.daemon(t)
	c.Watch.Reconcile(context.Background())
	if c.Watch.Ended("s_abcdefghij") {
		t.Fatal("a pane seconds old was marked ended")
	}
	if s := listed(t, c); s.Status == wire.StatusEnded || s.Exit != nil {
		t.Fatalf("listed after restart %+v, want no exit", s)
	}
}
