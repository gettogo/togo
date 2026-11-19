package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/wire"
)

// approvalAcrossRestart opens a screen prompt's approval, restarts the
// helper with the prompt still on screen, and answers the approval sent
// before the restart. It returns the approval before, those open after,
// and the answer's error.
func approvalAcrossRestart(t *testing.T) (*enginetest.Rig, wire.Approval, []wire.Approval, error) {
	ids, err := coverage.TerminalAgents()
	if err != nil || len(ids) == 0 {
		t.Fatalf("terminal agents %v: %v", ids, err)
	}
	e := newEnv(t, time.Hour)
	if _, err := sessiongen.Write(e.home, sessiongen.Terminal{}, sessiongen.Script{Seed: 1, Start: e.start, Cwd: e.cwd}); err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, ids[0])
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture("detect/testdata/generic/yn.txt")})
	r.Look()
	before := r.WaitPending(1, 3*time.Second)[0]
	r.Restart()
	r.Look()
	after := r.WaitPending(1, 3*time.Second)
	return r, before, after, r.Answer(before.RequestID, wire.DecisionApprove)
}

// TestKnownFault_ApprovalLostOnRestart documents a fault: approvals live
// only in the helper's memory (helper/internal/approval), so a helper
// restart (`togo update`) loses the open request. The prompt still on
// screen is read again as a new request with a new id, and an answer
// from the notification sent before the restart finds nothing.
//
// Correct: the request open before the restart is still open after it,
// under the same id, with the same options and expiry, and its answer is
// typed into the pane. This test passes while the fault is present.
func TestKnownFault_ApprovalLostOnRestart(t *testing.T) {
	_, before, after, err := approvalAcrossRestart(t)
	if after[0].RequestID == before.RequestID && err == nil {
		t.Fatal("the approval survived the restart and its answer was taken: the fault is fixed")
	}
}
