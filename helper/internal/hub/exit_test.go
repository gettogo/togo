package hub_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
	"github.com/gettogo/togo/helper/internal/wire"
)

const exitID = "s_abcdefghij"

func decorated(h *hub.Hub) session.Session {
	return h.Decorate([]session.Session{{ID: exitID, Managed: true, Status: "idle"}})[0]
}

func TestSetExitDecoratesUntilCleared(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	act := "Bash"
	h.SetStatus(exitID, "working", &act)
	one := 1
	h.SetExit(exitID, wire.StatusFailed, &wire.SessionExit{Code: &one, Reason: wire.ExitReasonExit})
	got := decorated(h)
	if got.Status != wire.StatusFailed || got.Activity != nil || got.Exit == nil || *got.Exit.Code != 1 || !h.Ended(exitID) {
		t.Fatalf("after exit %+v", got)
	}
	// Later output-derived status does not show while ended.
	h.SetStatus(exitID, "idle", nil)
	if got := decorated(h); got.Status != wire.StatusFailed {
		t.Fatalf("status %q while failed", got.Status)
	}
	h.ClearExit(exitID)
	if got := decorated(h); got.Status != "idle" || got.Exit != nil || h.Ended(exitID) {
		t.Fatalf("after clear %+v", got)
	}
}

func TestSetExitRejectsBadExit(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	bad := 300
	h.SetExit(exitID, wire.StatusFailed, &wire.SessionExit{Code: &bad, Reason: wire.ExitReasonExit})
	h.SetExit(exitID, "done", &wire.SessionExit{Reason: wire.ExitReasonError})
	h.SetExit(exitID, wire.StatusEnded, nil)
	if h.Ended(exitID) {
		t.Fatal("an invalid exit was recorded")
	}
}

func TestPendingApprovalOutranksExit(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	h.SetExit(exitID, wire.StatusFailed, &wire.SessionExit{Reason: wire.ExitReasonError})
	id := exitID
	h.Publish(wire.EventApprovalOpen, &id, wire.ApprovalOpenEvent{Approval: wire.Approval{RequestID: "r_abcdefghij", SessionID: &id}})
	if got := decorated(h); got.Status != "needs_you" || got.Exit != nil {
		t.Fatalf("with an approval pending %+v", got)
	}
}

func TestRelaunchMethodErrors(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	call := func(params string) error {
		_, err := h.SessionRelaunch(context.Background(), json.RawMessage(params))
		return err
	}
	if err := call(`{"id":"u_6d79"}`); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("unmanaged: %v", err)
	}
	if err := call(`{"id":"s_bbbbbbbbbb"}`); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	var ipe *session.InvalidParamsError
	if err := call(`{"id":"nope"}`); !errors.As(err, &ipe) {
		t.Errorf("bad id: %v", err)
	}
}

// TestHookExitWaitsForNewRunAfterRelaunch: after a relaunch, exit hooks
// are refused until the new run's first hook arrives.
func TestHookExitWaitsForNewRunAfterRelaunch(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	if !h.HookExitAllowed(exitID) {
		t.Fatal("refused before any relaunch")
	}
	h.SetExit(exitID, wire.StatusFailed, &wire.SessionExit{Reason: wire.ExitReasonUsageLimit})
	h.MarkRelaunched(exitID)
	if h.Ended(exitID) || h.HookExitAllowed(exitID) {
		t.Fatal("after relaunch: still ended, or exit hooks still allowed")
	}
	h.ClearExit(exitID)
	if !h.HookExitAllowed(exitID) {
		t.Fatal("exit hooks still refused after the new run's first hook")
	}
}

// Relaunches of one session run one at a time, and one that comes before
// the new run has exited or sent a hook is refused as running: while the
// new run starts, its pane can look as if the agent were gone, and a
// second respawn would kill it. Here the agent exits at once and nothing
// reports it, so only that rule refuses the second call.
func TestRelaunchOneRunAtATime(t *testing.T) {
	e := newEnv(t)
	prog := filepath.Join(e.svc.Path, "agent")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess, err := e.svc.Create(ctx, session.CreateOpts{Agent: wire.AgentCustom, Program: prog, Cwd: "~/proj"})
	if err != nil {
		t.Fatal(err)
	}
	settled := func() {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); ; {
			if gone, err := e.svc.AgentGone(ctx, sess.ID); err == nil && gone {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the pane never settled at the shell")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	params := json.RawMessage(`{"id":"` + sess.ID + `"}`)
	running := func(err error) bool {
		var ce *api.CodeError
		return errors.As(err, &ce) && ce.Code == rpc.CodeConflict
	}
	settled()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.hub.SessionRelaunch(ctx, params) })
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) || !running(errs[0]) && !running(errs[1]) {
		t.Fatalf("two relaunches at once: %v, %v; want one run and one running", errs[0], errs[1])
	}
	settled()
	if _, err := e.hub.SessionRelaunch(ctx, params); !running(err) {
		t.Fatalf("relaunch before the new run reported: %v, want running", err)
	}
	e.hub.SetExit(sess.ID, wire.StatusEnded, &wire.SessionExit{Code: new(int), Reason: wire.ExitReasonExit})
	if _, err := e.hub.SessionRelaunch(ctx, params); err != nil {
		t.Fatalf("relaunch after the new run exited: %v", err)
	}
}

// After a restart the first listing carries the stored exit, which the
// hub takes as restored; the next hook clears it like any exit.
func TestStoredExitReadBackAsRestored(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	code := 2
	stored := &session.SavedExit{Status: wire.StatusFailed, Exit: wire.SessionExit{Code: &code, Reason: wire.ExitReasonExit}}
	got := h.Decorate([]session.Session{{ID: exitID, Managed: true, Status: "idle", SavedExit: stored}})[0]
	if got.Status != wire.StatusFailed || got.Exit == nil || *got.Exit.Code != 2 || !got.ExitRestored || !h.Ended(exitID) {
		t.Fatalf("decorated %+v, want failed and restored", got)
	}
	h.ClearExit(exitID)
	// A listing taken before the option was removed does not bring it back.
	got = h.Decorate([]session.Session{{ID: exitID, Managed: true, Status: "idle", SavedExit: stored}})[0]
	if got.Status != "idle" || got.Exit != nil || h.Ended(exitID) {
		t.Fatalf("decorated %+v after a hook, want idle", got)
	}
	h.SetExit(exitID, wire.StatusEnded, &wire.SessionExit{Code: new(int), Reason: wire.ExitReasonExit})
	if got := decorated(h); got.ExitRestored {
		t.Fatal("an exit seen happening reads as restored")
	}
}

// An adapter's relaunch (new flags) kills the running agent, whose wrapper
// reports that exit at once: it must not mark the session ended.
func TestKilledRunExitAfterAdapterRelaunchIgnored(t *testing.T) {
	h := hub.New(&session.Service{Tmux: tmuxtest.New(t)}, nil)
	h.Relaunched(exitID)
	h.SetExit(exitID, wire.StatusFailed, &wire.SessionExit{Code: new(int), Reason: wire.ExitReasonExit})
	if h.Ended(exitID) {
		t.Fatal("the killed run's exit ended the session")
	}
}
