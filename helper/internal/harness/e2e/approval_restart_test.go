package e2e

import (
	"reflect"
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

// The approval open before a helper restart (`togo update`) is still open
// after it, under the same id, with the same options and expiry; the
// prompt read again opens nothing new, and the answer from the
// notification sent before the restart is typed into the pane.
func TestApprovalSurvivesRestart(t *testing.T) {
	r, before, after, err := approvalAcrossRestart(t)
	if len(after) != 1 || after[0].RequestID != before.RequestID {
		t.Fatalf("open after the restart: %+v, before: %s", after, before.RequestID)
	}
	if !reflect.DeepEqual(after[0].Options, before.Options) || !after[0].ExpiresAt.Equal(*before.ExpiresAt) {
		t.Errorf("after the restart %+v, before %+v", after[0], before)
	}
	if err != nil {
		t.Fatalf("answer after the restart: %v", err)
	}
	if r.Term.Dialog() != nil {
		t.Fatal("the prompt is still on screen after the answer")
	}
	if got := r.Closed()[before.RequestID]; got != wire.ByPhone {
		t.Errorf("closed as %q, want %q", got, wire.ByPhone)
	}
}
