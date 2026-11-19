package generic

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Each lifecycle event, on a session holding live state: the agent at
// work, a prompt queued behind it, its permission prompt open on the
// phone, and a subagent still running where the agent has them. After
// the event nothing stays live that the event ended (rule 1: every set
// needs every clear), and nothing is lost that should remain.

func TestLifecycle(t *testing.T) {
	events := []struct {
		name string
		run  func(t *testing.T, lv *live)
	}{
		{"agent_exits", agentExits},
		{"relaunch", relaunch},
		{"session_removed", sessionRemoved},
		{"turn_end", turnEnd},
		{"connection_drop", connectionDrop},
		{"interrupt", interrupt},
	}
	for _, agent := range profiles(t) {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			for _, ev := range events {
				t.Run(ev.name, func(t *testing.T) {
					t.Parallel()
					if !declare(t, agent, "lifecycle/"+ev.name) {
						return
					}
					lv := start(t, agent)
					lv.makeLive()
					ev.run(t, lv)
				})
			}
		})
	}
}

// agentExits: the agent's process ends with everything open. Its turn,
// tools and subagents end, the queued prompt is withdrawn and never
// typed, and its permission prompt closes with it.
func agentExits(t *testing.T, lv *live) {
	r := lv.r
	r.Exit()
	lv.approvalClosed(wire.ResolutionDesk)
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		it, ok := item(st, lv.queued)
		return ok && it.User != nil && it.User.Withdrawn
	})
	nothingRunning(t, st)
	if q, _ := item(st, lv.queued); q.User == nil || q.User.Queued || !q.User.Withdrawn {
		t.Errorf("the queued prompt is not withdrawn: %+v", q.User)
	}
	if slices.Contains(r.Term.Submitted(), lv.queuedText) {
		t.Error("the queued prompt was typed into a pane whose agent had exited")
	}
}

// relaunch: the agent exits mid-turn and is started again in the same
// pane. What the old run left open ends: its turn, tools and subagents,
// its permission prompt, the prompt queued behind it. The new run is not
// given that prompt. (That the new run is still taken as working is
// TestKnownFault_RelaunchKeepsDeadTurnWorking.)
func relaunch(t *testing.T, lv *live) {
	r := lv.r
	r.Exit()
	lv.approvalClosed(wire.ResolutionDesk)
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		it, ok := item(st, lv.queued)
		return ok && it.User != nil && it.User.Withdrawn
	})
	r.Relaunch()
	r.SetHubStatus("idle")
	st := r.Settle(3 * time.Second)
	for _, it := range st.Items {
		if latest, _ := item(st, it.ID); latest.Seq == it.Seq && (it.Tool != nil && running(it.Tool.State) ||
			it.Agent != nil && running(it.Agent.State) || it.Turn != nil && running(it.Turn.State)) {
			t.Errorf("%s from the run that exited is still running", it.ID)
		}
	}
	if slices.Contains(r.Term.Submitted(), lv.queuedText) {
		t.Error("the prompt queued for the run that exited was typed into the new one")
	}
}

func running(s string) bool { return s == harness.StateRunning || s == harness.StateWaiting }

// sessionRemoved: the session goes away. Its approval closes as gone,
// the engine stops serving it, and the queued prompt is never typed.
func sessionRemoved(t *testing.T, lv *live) {
	r := lv.r
	r.Remove()
	lv.approvalClosed(wire.ResolutionGone)
	eventually(t, 3*time.Second, "the engine to forget the session", func() bool {
		_, ok := r.Engine.Summary(r.ID)
		return !ok
	})
	r.Term.CloseDialog()
	r.SetStatus("idle")
	time.Sleep(300 * time.Millisecond)
	if slices.Contains(r.Term.Submitted(), lv.queuedText) {
		t.Error("the queued prompt of a removed session was typed")
	}
}

// turnEnd: the prompt is answered at the desk and the turn ends. The
// approval closes as answered at the desk, and the queued prompt is
// typed once and shown as sent.
func turnEnd(t *testing.T, lv *live) {
	r := lv.r
	r.Term.CloseDialog()
	r.Look()
	lv.approvalClosed(wire.ResolutionDesk)
	lv.endTurn()
	r.SetHubStatus("idle")
	eventually(t, 10*time.Second, "the queued prompt to be typed", func() bool {
		return slices.Contains(r.Term.Submitted(), lv.queuedText)
	})
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		it, ok := item(st, lv.queued)
		return ok && it.User != nil && !it.User.Queued
	})
	if q, _ := item(st, lv.queued); q.User == nil || q.User.Withdrawn {
		t.Errorf("the queued prompt is not shown as sent: %+v", q.User)
	}
	if n := count(r.Term.Submitted(), lv.queuedText); n != 1 {
		t.Errorf("the queued prompt was typed %d times", n)
	}
}

// connectionDrop: the phone loses its connection while the session
// holds live state; meanwhile the prompt is answered at the desk and the
// turn ends. Back with its old cursor, the phone shows every item as the
// helper has it.
func connectionDrop(t *testing.T, lv *live) {
	r := lv.r
	lv.phone.Drop()
	r.Term.CloseDialog()
	r.Look()
	lv.approvalClosed(wire.ResolutionDesk)
	lv.endTurn()
	r.SetHubStatus("idle")
	eventually(t, 10*time.Second, "the queued prompt to be typed", func() bool {
		return slices.Contains(r.Term.Submitted(), lv.queuedText)
	})
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		it, ok := item(st, lv.queued)
		return ok && it.User != nil && !it.User.Queued
	})
	lv.phone.Return()
	enginetest.Fail(t, "after the reconnect", enginetest.Resynced(lv.phone, st))
	if q := lv.phone.Items()[lv.queued]; q.User == nil || q.User.Queued {
		t.Errorf("the phone still shows the prompt as queued: %+v", q.User)
	}
}

// interrupt: the phone interrupts the turn. The adapter's interrupt key
// reaches the pane (it also dismisses the open prompt, as Escape does in
// the agents' own dialogs); the agent stops; nothing is left running and
// the queued prompt is not lost.
func interrupt(t *testing.T, lv *live) {
	r := lv.r
	var once sync.Once
	stop := func() { once.Do(func() { lv.endTurn(); r.SetHubStatus("idle") }) }
	r.Term.OnInterrupt = func(string) { go stop() }
	r.Term.OnKey = func(d *enginetest.Dialog, key string, _ bool) bool {
		if key == "Escape" || key == "C-c" {
			r.Term.CloseDialog()
			go func() { r.Look(); stop() }()
			return true
		}
		return false
	}
	if _, err := r.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	keys := r.Term.KeysSent()
	if !slices.Contains(keys, "Escape") && !slices.Contains(keys, "C-c") {
		t.Fatalf("no interrupt key reached the pane: %v", keys)
	}
	r.Look()
	lv.approvalClosed(wire.ResolutionDesk)
	eventually(t, 10*time.Second, "the queued prompt to be typed", func() bool {
		return slices.Contains(r.Term.Submitted(), lv.queuedText)
	})
	st := r.Settle(3 * time.Second)
	if q, ok := item(st, lv.queued); !ok || q.User == nil || q.User.Withdrawn {
		t.Errorf("the queued prompt was lost: %+v", q.User)
	}
}

func count(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}
