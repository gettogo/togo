package support

import (
	"fmt"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Hook-driven agents (copilot, droid, cursor, amp) reach the engine as
// hook payloads. deliver does what `togo hook <agent> <event>` does with a
// call: the agent's dialect reduces it to the canonical event and its
// redacted payload, which the daemon puts on the session's hook bus. The
// rig's engine reads the bus as it does in the daemon. The bus keeps no
// history for a restarted engine, as the daemon's does not.

// hookEnv is the environment a hook process has: it knows its Togo session.
func HookEnv(r *enginetest.Rig) map[string]string { return map[string]string{"TOGO_SESSION": r.ID} }

// deliver applies each call's file lines, then delivers the call.
func Deliver(t testing.TB, r *enginetest.Rig, agent string, calls ...sessiongen.HookCall) {
	t.Helper()
	d, ok := hookstyle.For(agent)
	if !ok {
		t.Fatalf("no hook dialect for %s", agent)
	}
	for _, c := range calls {
		if err := c.Apply(); err != nil {
			t.Fatal(err)
		}
		f, err := hookstyle.Reduce(d, c.Event, c.JSON(), HookEnv(r))
		if err != nil {
			t.Fatalf("%s %s: %v", agent, c.Event, err)
		}
		if len(f.Payload) == 0 {
			t.Fatalf("%s %s: the hook forwarded no payload", agent, c.Event)
		}
		// A call that follows lines the agent wrote to its log is read
		// with the log as it was then: wait until the engine has read it
		// before the next lines are written. The wait ends when the call
		// has drawn an item, or, for a call that draws nothing (a
		// duplicate, a stop after a restart), when the hook readers have
		// finished it: a long fixed wait here costs that long on every
		// such call, and ten times that under the race detector. The
		// limit is for a machine too loaded for either to happen.
		if len(c.Writes) == 0 {
			r.DeliverAt(f.Hook, f.Payload, c.At)
			continue
		}
		before := r.Cursor()
		r.DeliverAt(f.Hook, f.Payload, c.At)
		r.WaitHook(before, 15*time.Second)
	}
}

// turnOf finds the item of the turn that began at call c (a prompt call).
func TurnID(c sessiongen.HookCall) string { return fmt.Sprintf("turn-%d", c.At.UnixMilli()) }

// turnDone waits for the turn a call began to leave the running state.
func TurnDone(c sessiongen.HookCall) func(enginetest.State) bool {
	id := TurnID(c)
	return func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.ID == id && it.Turn != nil && !Running(it.Turn.State) {
				return true
			}
		}
		return false
	}
}

func Running(s string) bool { return s == harness.StateRunning || s == harness.StateWaiting }

// settle waits for the state to stop changing after a delivery, and for ok.
func Settle(r *enginetest.Rig, ok func(enginetest.State) bool) enginetest.State {
	return r.SettleUntil(15*time.Second, ok)
}
