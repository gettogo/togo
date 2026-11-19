package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The terminal row of the matrix stands for any other command-line agent in
// the terminal view: the agents whose adapters declare no chat (Auggie,
// Kiro CLI, OpenHands, the shell). The engine has no chat to follow, and
// the rules that remain are the lifecycle ones and the prompts read off
// the screen. The chat stays empty, nothing works or waits, and none of
// these events breaks the session. Each scenario runs for every such
// agent, found from the adapter files.

// terminalEach runs f once for each agent the terminal row stands for.
func terminalEach(t *testing.T, f func(t *testing.T, r *enginetest.Rig)) {
	t.Helper()
	ids, err := coverage.TerminalAgents()
	if err != nil || len(ids) == 0 {
		t.Fatalf("terminal agents %v: %v", ids, err)
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, time.Hour)
			if _, err := sessiongen.Write(e.home, sessiongen.Terminal{}, sessiongen.Script{Seed: 1, Start: e.start, Cwd: e.cwd}); err != nil {
				t.Fatal(err)
			}
			f(t, e.rig(t, id))
		})
	}
}

func quietState(t *testing.T, r *enginetest.Rig, where string) enginetest.State {
	t.Helper()
	st := r.Settle(fileLimit)
	if len(st.Items) != 0 || st.Mission.Summary.Working || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("%s: %d items, summary %+v; want an empty chat and nothing working", where, len(st.Items), st.Mission.Summary)
	}
	return st
}

func TestTerminalHelperRestart(t *testing.T) {
	coverage.Covers(t, "generated", "terminal/lifecycle/helper_restart")
	terminalEach(t, func(t *testing.T, r *enginetest.Rig) {
		before := quietState(t, r, "start")
		for i := 0; i < 3; i++ {
			r.Restart()
			after := quietState(t, r, "after restart")
			fail(t, "restart", enginetest.Restarted(before, after), &sessiongen.Layout{})
		}
	})
}

func TestTerminalExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "terminal/lifecycle/agent_exits", "terminal/lifecycle/relaunch")
	terminalEach(t, func(t *testing.T, r *enginetest.Rig) {
		r.SetStatus("working")
		r.Settle(fileLimit)
		r.SetEnded(true)
		gone := r.Settle(fileLimit)
		if gone.Mission.Summary.Working {
			t.Errorf("summary %+v after the agent exited", gone.Mission.Summary)
		}
		r.SetEnded(false)
		r.SetStatus("")
		back := r.Settle(fileLimit)
		if back.Mission.Summary.Working || back.Mission.Summary.NeedsYou != 0 {
			t.Errorf("summary %+v after the relaunch", back.Mission.Summary)
		}
		// A second exit and relaunch behave the same.
		r.SetEnded(true)
		r.Settle(fileLimit)
		r.SetEnded(false)
		r.SetStatus("")
		quietState(t, r, "after the second relaunch")
	})
}

func TestTerminalConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "terminal/lifecycle/connection_drop")
	terminalEach(t, func(t *testing.T, r *enginetest.Rig) {
		ctx := context.Background()
		sub := &quietSub{done: make(chan struct{})}
		first, err := r.Engine.Open(ctx, sub, enginetest.SessionID, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Engine.Close(ctx, sub, enginetest.SessionID)
		close(sub.done)
		r.SetEnded(true)
		r.SetEnded(false)
		sub2 := &quietSub{done: make(chan struct{})}
		defer close(sub2.done)
		back, err := r.Engine.Open(ctx, sub2, enginetest.SessionID, first.Cursor, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if back.Epoch != first.Epoch {
			t.Errorf("the epoch changed from %q to %q while the client was away", first.Epoch, back.Epoch)
		}
		seen := map[string]bool{}
		for _, it := range back.Items {
			if seen[it.ID] {
				t.Errorf("%s comes twice after the reconnect", it.ID)
			}
			seen[it.ID] = true
		}
	})
}

func TestTerminalSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "terminal/lifecycle/session_removed")
	terminalEach(t, func(t *testing.T, r *enginetest.Rig) {
		r.SetStatus("working")
		r.Settle(fileLimit)
		r.Engine.Forget(enginetest.SessionID)
		st := r.Read()
		if len(st.Items) != 0 || st.Mission.Summary.Working {
			t.Errorf("after the removal: %d items, summary %+v", len(st.Items), st.Mission.Summary)
		}
		for _, sec := range st.Mission.Sections {
			if len(sec.Rows) != 0 {
				t.Errorf("section %s still has %d rows", sec.Kind, len(sec.Rows))
			}
		}
		// The same id served again starts clean.
		r.SetStatus("")
		r.Start()
		quietState(t, r, "served again")
	})
}

// A prompt drawn on the screen (a numbered menu, a yes/no question) is
// read by the screen watcher and reaches the phone as a question, whichever
// command-line agent draws it. (Answering the other choice of a menu moves
// a cursor first, which needs a capture of the moved screen; there is none
// for these agents, so the menu is answered with its first choice.) The phone's answer is typed into the pane,
// the prompt leaves, and the question closes as the phone's. A second
// answer is refused.
func TestTerminalScreenPrompts(t *testing.T) {
	coverage.Covers(t, "generated", "terminal/capability/screen_prompt")
	for _, c := range []struct{ name, screen, decision string }{
		{"menu_approve", "detect/testdata/auggie/real-perm-80.txt", wire.DecisionApprove},
		{"yes_no_approve", "detect/testdata/generic/yn.txt", wire.DecisionApprove},
		{"yes_no_deny", "detect/testdata/generic/yn.txt", wire.DecisionDeny},
	} {
		t.Run(c.name, func(t *testing.T) {
			terminalEach(t, func(t *testing.T, r *enginetest.Rig) {
				r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(c.screen)})
				r.Look()
				p := r.WaitPending(1, 3*time.Second)[0]
				if err := r.Answer(p.RequestID, c.decision); err != nil {
					t.Fatalf("answer: %v", err)
				}
				if r.Term.Dialog() != nil {
					t.Fatal("the prompt is still on screen after the phone's answer")
				}
				if got := r.Closed()[p.RequestID]; got != wire.ByPhone {
					t.Errorf("closed as %q, want %q", got, wire.ByPhone)
				}
				r.Look()
				if len(r.Pending()) != 0 {
					t.Errorf("%d approvals still open", len(r.Pending()))
				}
				if err := r.Answer(p.RequestID, c.decision); err == nil {
					t.Error("a second answer to a closed prompt was taken")
				}
			})
		})
	}
}
