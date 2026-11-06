package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// exitSink records exits like the hub: a session stays ended until
// cleared.
type exitSink struct {
	*harness
	status string
	exit   *wire.SessionExit
	sets   int
}

func (x *exitSink) SetExit(_ string, status string, exit *wire.SessionExit) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.status, x.exit = status, exit
	x.sets++
}

func (x *exitSink) Ended(string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.status != ""
}

// newExitHarness is newHarness with an exit sink and a pane check that
// reports gone.
func newExitHarness(t *testing.T) *exitSink {
	t.Helper()
	h := newHarness(t)
	x := &exitSink{harness: h}
	h.w.cfg.Status = x
	h.w.agentGone = func(context.Context, string) (bool, error) { return true, nil }
	return x
}

func (x *exitSink) exitWith(t *testing.T, code int) {
	t.Helper()
	if err := x.w.Signal(context.Background(), wire.Signal{SessionID: sid, Kind: wire.SignalExit, Code: &code}); err != nil {
		t.Fatal(err)
	}
}

// idleWindows runs n idle windows of silence, ticking through each.
func (x *exitSink) idleWindows(n int) {
	for range n * 4 {
		x.advance(x.w.idleWindow() / 4)
		x.w.Tick(context.Background())
	}
}

func (x *exitSink) doneCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	n := 0
	for _, e := range x.events {
		if e.Level == wire.LevelDone {
			n++
		}
	}
	return n
}

func (x *exitSink) want(t *testing.T, status, reason string, code int) {
	t.Helper()
	if x.status != status || x.exit == nil || x.exit.Reason != reason || x.exit.Code == nil || *x.exit.Code != code {
		t.Fatalf("exit %q %+v, want %s/%s code %d", x.status, x.exit, status, reason, code)
	}
	if err := x.exit.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExitNonZeroIsFailedNotDone(t *testing.T) {
	x := newExitHarness(t)
	x.signal(t, wire.SignalOutput, "")
	x.exitWith(t, 1)
	x.want(t, wire.StatusFailed, wire.ExitReasonExit, 1)
	x.idleWindows(3)
	if n := x.doneCount(); n != 0 {
		t.Fatalf("%d done notifications after a failed exit", n)
	}
	for _, s := range x.statuses[1:] {
		t.Fatalf("status %s set after the exit", s)
	}
}

func TestExitZeroIsEnded(t *testing.T) {
	x := newExitHarness(t)
	x.exitWith(t, 0)
	x.want(t, wire.StatusEnded, wire.ExitReasonExit, 0)
}

func TestInterruptIsEnded(t *testing.T) {
	for _, code := range []int{130, 143} {
		x := newExitHarness(t)
		x.exitWith(t, code)
		x.want(t, wire.StatusEnded, wire.ExitReasonInterrupted, code)
	}
	x := newExitHarness(t)
	x.exitWith(t, 137)
	x.want(t, wire.StatusFailed, wire.ExitReasonExit, 137)
}

func TestExitWithoutCodeRejected(t *testing.T) {
	x := newExitHarness(t)
	if err := x.w.Signal(context.Background(), wire.Signal{SessionID: sid, Kind: wire.SignalExit}); err == nil {
		t.Fatal("exit without a code accepted")
	}
	if x.sets != 0 {
		t.Fatal("recorded an exit")
	}
}

func TestShellOutputAfterExitSendsNoDone(t *testing.T) {
	x := newExitHarness(t)
	x.signal(t, wire.SignalOutput, "")
	x.exitWith(t, 0)
	before := len(x.statuses)
	for range 3 {
		x.signal(t, wire.SignalOutput, "")
		x.idleWindows(2)
	}
	if len(x.statuses) != before {
		t.Fatalf("statuses after the exit: %v", x.statuses[before:])
	}
	if n := x.doneCount(); n != 0 {
		t.Fatalf("%d done notifications from shell output", n)
	}
	if x.status != wire.StatusEnded {
		t.Fatalf("status %q, want ended", x.status)
	}
}

func TestBellAfterExitStillNotifies(t *testing.T) {
	x := newExitHarness(t)
	x.exitWith(t, 1)
	x.signal(t, wire.SignalBell, "")
	if len(x.events) != 1 || x.events[0].Level != wire.LevelNeedsYou {
		t.Fatalf("events %+v, want one needs_you", x.events)
	}
}

// TestExitIgnoredWhileAgentRuns: a report that arrives while the pane
// still shows the agent changes nothing.
func TestExitIgnoredWhileAgentRuns(t *testing.T) {
	x := newExitHarness(t)
	x.w.agentGone = func(context.Context, string) (bool, error) { return false, nil }
	x.signal(t, wire.SignalOutput, "")
	x.exitWith(t, 1)
	if x.sets != 0 {
		t.Fatalf("recorded %q while the agent runs", x.status)
	}
	x.idleWindows(1)
	if x.doneCount() != 1 {
		t.Fatal("the session no longer settles as before")
	}
}

// TestHookAfterExitClearsEnded drives the real hub and hook handler: an
// exit makes the session ended, a prompt hook makes it working again.
func TestHookAfterExitClearsEnded(t *testing.T) {
	x := newExitHarness(t)
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	x.w.cfg.Status = h
	x.exitWith(t, 0)
	if !x.w.Ended(sid) || !h.Ended(sid) {
		t.Fatal("not ended after the exit")
	}
	lh := &local.Handlers{Hub: h, Pub: x}
	id := sid
	for _, ev := range []string{local.EventMeta, local.EventNotify, local.EventPrompt} {
		raw, _ := json.Marshal(wire.HookEvent{Agent: "claude", Event: ev, SessionID: &id})
		if _, err := lh.Methods()[wire.MethodLocalHook](context.Background(), raw); err != nil {
			t.Fatal(err)
		}
		if ev != local.EventPrompt && !h.Ended(sid) {
			t.Fatalf("%s hook cleared the exit", ev)
		}
	}
	if x.w.Ended(sid) {
		t.Fatal("still ended after a prompt hook")
	}
	got := h.Decorate([]session.Session{{ID: sid, Managed: true, Status: "idle"}})[0]
	if got.Status != "working" || got.Exit != nil {
		t.Fatalf("status %q exit %+v, want working", got.Status, got.Exit)
	}
}

func TestAgentGone(t *testing.T) {
	const togo = "/opt/to go/togo"
	long := "/usr/local/bin/togo-helper-build"
	for _, c := range []struct {
		name string
		fg   tmux.PaneForeground
		path string
		want bool
	}{
		{"dead pane", tmux.PaneForeground{Dead: true}, togo, true},
		{"shell at prompt", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 10, Name: "zsh"}}}, togo, true},
		{"wrapper reporting", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 12, Name: "togo"}}}, togo, true},
		{"wrapper reporting, no job control", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 10, Name: "sh"}, {PID: 12, Name: "togo"}}}, togo, true},
		{"linux short name", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 12, Name: "togo-helper-bui"}}}, long, true},
		{"agent running", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 11, Name: "claude"}}}, togo, false},
		{"agent sending the report", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 11, Name: "node"}, {PID: 13, Name: "togo"}}}, togo, false},
		{"agent without job control", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 10, Name: "sh"}, {PID: 11, Name: "claude"}}}, togo, false},
		{"unknown pane", tmux.PaneForeground{}, togo, false},
		{"no helper path", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 12, Name: "."}}}, "", false},
	} {
		if got := session.AgentGone(c.fg, c.path); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSpoofedExitIgnoredOnRealPane runs a stand-in agent in a real pane.
// A report sent while it runs is ignored; once it exits and the pane is
// back at a shell, the same report counts.
func TestSpoofedExitIgnoredOnRealPane(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	dir := t.TempDir()
	flag := filepath.Join(dir, "stop")
	agent := `while [ ! -e "$1" ]; do /bin/sleep 0.05; done; exec /bin/sh -i`
	err := c.NewSession(ctx, tmux.NewSessionOpts{Name: sid, Dir: dir, Command: []string{"/bin/sh", "-c", agent, "sh", flag},
		Env: map[string]string{"ENV": "", "PS1": "$ "}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	x := &exitSink{harness: h}
	h.w.cfg = Config{Tmux: c, TogoPath: "/opt/togo/bin/togo", Status: x, Pub: h}
	h.w.agentGone = h.w.paneGone
	// Wait for the stand-in to be in the foreground.
	for deadline := time.Now().Add(10 * time.Second); ; {
		fg, err := c.Foreground(ctx, sid)
		if err == nil && len(fg.Foreground) > 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stand-in agent never ran: %+v %v", fg, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	x.exitWith(t, 1)
	if x.sets != 0 {
		t.Fatalf("a report for a live agent counted: %q", x.status)
	}
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); x.sets == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the report never counted once the agent was gone")
		}
		time.Sleep(20 * time.Millisecond)
		x.exitWith(t, 1)
	}
	x.want(t, wire.StatusFailed, wire.ExitReasonExit, 1)
	// A session that no longer exists is ignored without an error.
	other := "s_bbbbbbbbbb"
	code := 1
	if err := h.w.Signal(ctx, wire.Signal{SessionID: other, Kind: wire.SignalExit, Code: &code}); err != nil || x.sets != 1 {
		t.Fatalf("missing session: err %v, sets %d", err, x.sets)
	}
}

// TestHookFailureAfterOutputSendsNoDone: Claude prints its error, then the
// StopFailure hook marks the session failed while the output is still
// waiting for its idle check. The silence that follows is not a done.
func TestHookFailureAfterOutputSendsNoDone(t *testing.T) {
	x := newExitHarness(t)
	x.signal(t, wire.SignalOutput, "")
	before := len(x.statuses)
	// Recorded the way the hook handler records it, not through Signal.
	x.SetExit(sid, wire.StatusFailed, &wire.SessionExit{Reason: wire.ExitReasonUsageLimit})
	x.idleWindows(3)
	if n := x.doneCount(); n != 0 {
		t.Fatalf("%d done notifications after a usage limit", n)
	}
	if len(x.statuses) != before {
		t.Fatalf("statuses after the failure: %v", x.statuses[before:])
	}
}

// restoreSink is an exit sink that records restored exits.
type restoreSink struct {
	*exitSink
	restored int
}

func (r *restoreSink) RestoreExit(id string, status string, exit *wire.SessionExit) {
	r.restored++
	r.SetExit(id, status, exit)
}

// newStartHarness lists one agent session and reports its pane through
// gone, one answer per scan.
func newStartHarness(t *testing.T, gone ...bool) *restoreSink {
	t.Helper()
	h := newHarness(t)
	h.sessions[0].Command = []string{"aider"}
	r := &restoreSink{exitSink: &exitSink{harness: h}}
	h.w.cfg.Status = r
	scan := 0
	h.w.settledGone = func(context.Context, string) (bool, error) {
		g := scan < len(gone) && gone[scan]
		scan++
		return g, nil
	}
	return r
}

// An agent found gone at start is marked ended only when the first two
// scans both see it gone; later scans never mark it.
func TestStartMarksGoneAgentAfterTwoScans(t *testing.T) {
	ctx := context.Background()
	r := newStartHarness(t, true, true)
	r.w.Reconcile(ctx)
	if r.restored != 0 {
		t.Fatal("marked ended after one scan")
	}
	r.w.Reconcile(ctx)
	if r.restored != 1 || !r.w.Ended(sid) {
		t.Fatalf("restored %d, want ended after two scans", r.restored)
	}
	if r.status != wire.StatusEnded || r.exit == nil || r.exit.Code != nil || r.exit.Reason != wire.ExitReasonExit {
		t.Fatalf("exit %q %+v, want ended with no code", r.status, r.exit)
	}

	r = newStartHarness(t, true, false, true, true)
	for range 4 {
		r.w.Reconcile(ctx)
	}
	if r.restored != 0 {
		t.Fatal("an agent seen running on the second scan was marked ended")
	}
}

// A pane that just started shows only its login shell until the agent
// starts, so it never counts as the agent gone; a dead pane always does.
func TestSettledGoneWaitsForThePane(t *testing.T) {
	shell := []tmux.Proc{{PID: 10, Name: "zsh"}}
	for _, c := range []struct {
		name string
		fg   tmux.PaneForeground
		want bool
	}{
		{"new pane at its shell", tmux.PaneForeground{PanePID: 10, Foreground: shell, PaneAge: 2 * time.Second}, false},
		{"settled pane at its shell", tmux.PaneForeground{PanePID: 10, Foreground: shell, PaneAge: 10 * time.Second}, true},
		{"settled pane with the agent", tmux.PaneForeground{PanePID: 10, Foreground: []tmux.Proc{{PID: 11, Name: "aider"}}, PaneAge: time.Hour}, false},
		{"dead pane", tmux.PaneForeground{Dead: true}, true},
	} {
		if got := SettledGone(c.fg, "/opt/togo/togo"); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
