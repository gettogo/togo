package support

import (
	"fmt"
	"strings"
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
	// pending: an earlier call went in without a wait, so its hook may not
	// have been read yet.
	pending := true
	for _, c := range calls {
		if err := hookStep(t, r, d, agent, c, &pending); err != nil {
			t.Fatal(err)
		}
	}
}

func hookStep(t testing.TB, r *enginetest.Rig, d hookstyle.Dialect, agent string, c sessiongen.HookCall, pending *bool) error {
	t.Helper()
	if len(c.Writes) > 0 && *pending {
		// A hook reads the agent's log as it is when the hook is read, and
		// a session's first hook starts reading at the log's end: the
		// lines this call writes must not be there before the hooks that
		// came earlier have been read. Calls that draw nothing, or have
		// no lines of their own, are not waited for below, so wait here.
		if !r.WaitHooksIdle(15 * time.Second) {
			return fmt.Errorf("%s: the hook readers did not finish the earlier calls", agent)
		}
		*pending = false
	}
	if err := c.Apply(); err != nil {
		return err
	}
	f, err := hookstyle.Reduce(d, c.Event, c.JSON(), HookEnv(r))
	if err != nil {
		return fmt.Errorf("%s %s: %v", agent, c.Event, err)
	}
	if len(f.Payload) == 0 {
		return fmt.Errorf("%s %s: the hook forwarded no payload", agent, c.Event)
	}
	// A call that follows lines the agent wrote to its log is read with
	// the log as it was then: wait until the engine has read it before the
	// next lines are written. The wait ends when the call has drawn an
	// item, or, for a call that draws nothing (a duplicate, a stop after a
	// restart), when the hook readers have finished it: a long fixed wait
	// here costs that long on every such call, and ten times that under
	// the race detector. The limit is for a machine too loaded for either
	// to happen.
	if len(c.Writes) == 0 {
		r.DeliverAt(f.Hook, f.Payload, c.At)
		// Only the events that open or close a session matter to the next
		// call's lines: a tool's result changes no log offset.
		if strings.Contains(strings.ToLower(c.Event), "session") {
			*pending = true
		}
		return nil
	}
	before := r.Cursor()
	r.DeliverAt(f.Hook, f.Payload, c.At)
	r.WaitHook(before, 15*time.Second)
	return nil
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
