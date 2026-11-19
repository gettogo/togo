package generic

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// TestKnownFault_RelaunchKeepsDeadTurnWorking: an agent that exits in the
// middle of a turn and is started again in the same pane is still taken
// as working. The exit marks the turn's items interrupted
// (engine/sess.go agentExitedLocked), but the mappers' own state still
// says a turn runs, and the session's working state is that state ORed
// with the hub's (engine/sess.go stateLocked). Once the hub says the new
// run is up, the dead turn counts again: the summary says working and a
// prompt sent now from the phone is refused as busy, until the new run
// writes a record of its own.
//
// Correct: the exit ends the turn for the mappers as well, so the new run
// is idle and takes a prompt at once.
func TestKnownFault_RelaunchKeepsDeadTurnWorking(t *testing.T) {
	for _, agent := range profiles(t) {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			lv := start(t, agent)
			r := lv.r
			r.Exit()
			r.Settle(3 * time.Second)
			r.Relaunch()
			r.SetHubStatus("idle")
			st := r.Settle(3 * time.Second)
			if !st.Mission.Summary.Working {
				t.Fatal("the fault is gone: the relaunched agent is idle; move relaunch to the lifecycle scenarios")
			}
			if _, err := r.Send("After the relaunch", harness.SendNow); !busy(err) {
				t.Fatalf("the fault changed: send now after the relaunch gave %v, the fault gives busy", err)
			}
		})
	}
}
