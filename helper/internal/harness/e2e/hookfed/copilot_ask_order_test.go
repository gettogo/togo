package hookfed

import (
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestKnownFault_AskOfferAnsweredWhileOpeningShowsAsApproval documents a
// fault in the engine's offer of an open question as an ask of its own
// (engine/feedapproval.go). After offerGrace, offerAfterGrace checks the
// question is open, lets go of the session lock and calls OpenAsk. The
// store publishes the ask's open event, which the engine handles on a
// goroutine of its own (engine.go Observe, builtin.go addApproval). If the
// question is answered at the desk after that check and before both the
// open event is handled and OpenAsk returns:
//
//   - offerLocked (feedapproval.go) finds the offer with no request id
//     yet, drops it and closes nothing;
//   - addApproval finds the ask still pending, and ownerLocked
//     (needs.go) finds no open question to give it as owner, so the ask
//     is drawn as an approval of its own, with a Needs row;
//   - offerPermission then sees the offer gone and closes the ask as
//     answered at the desk.
//
// The chat keeps an approval card nobody was asked, resolved at the desk,
// beside the answered question. Asks are not kept across a restart
// (approval/persist.go), so after one the card is gone: the
// "restart-lost-item approval-r_..." TestCopilotRestartsAtSeededPoints
// reported on a loaded runner. Here the two late steps are held by the rig
// (HoldApprovalOpens, GateAsks) so the order is the same every run.
//
// Correct: an ask opened for a question that is no longer open is not
// drawn (or is drawn owned by the question, folded into its card), and
// opens no Needs row.
//
// Trigger: a question answered at the terminal at the moment its offer
// opens (offerGrace, 2 s, after it appeared), with the helper's goroutines
// late to run.
func TestKnownFault_AskOfferAnsweredWhileOpeningShowsAsApproval(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, time.Hour)
	ask := &sessiongen.HookAsk{Prompt: "Which one?", Options: []string{"Red", "Green"}, Answer: "Green"}
	l := write(t, e, copilotAgent, []sessiongen.Turn{{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Ask: ask}}, {Say: 20}})
	at := -1
	for i, c := range l.Hooks {
		for _, k := range copilotAgent.classify(c) {
			if k.what == "ask" && at < 0 {
				at = i
			}
		}
	}
	if at < 0 {
		t.Fatal("the script asks no question")
	}
	r := e.Rig(t, "copilot")
	entered, releaseAsk := r.GateAsks()
	releaseOpens := r.HoldApprovalOpens()
	t.Cleanup(func() { releaseOpens(); releaseAsk() })
	run(t, r, copilotAgent, l.Hooks[:at+1])
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the open question was never offered")
	}
	// Answered at the desk while the offer opens.
	end := at + 1
	for end < len(l.Hooks) && l.Hooks[end].Turn == l.Hooks[at].Turn {
		end++
	}
	run(t, r, copilotAgent, l.Hooks[at+1:end])
	answered := func(st enginetest.State) bool {
		qs := support.Items(st, support.OfKind(harness.KindQuestion))
		return len(qs) == 1 && len(qs[0].Question.Answered) > 0
	}
	if !answered(r.SettleUntil(30*time.Second, answered)) {
		t.Fatal("the question was not answered")
	}
	releaseOpens()
	approvals := func(st enginetest.State) []harness.Item {
		return support.Items(st, support.OfKind(harness.KindApproval))
	}
	r.SettleUntil(5*time.Second, func(st enginetest.State) bool { return len(approvals(st)) > 0 })
	releaseAsk()
	st := r.SettleUntil(30*time.Second, func(st enginetest.State) bool {
		as := approvals(st)
		return len(r.Pending()) == 0 && len(as) > 0 && as[0].Approval.Resolved != ""
	})
	as := approvals(st)
	if len(as) != 1 || as[0].Approval.Owner != "" {
		t.Fatalf("the fault is fixed: approval items %+v; delete this test", as)
	}
	if as[0].Approval.Resolved != "desk" || len(r.Pending()) != 0 {
		t.Errorf("the fault changed: %+v, %d pending; the fault gives one closed at the desk", as[0].Approval, len(r.Pending()))
	}
	// After a restart the card is gone.
	r.Restart()
	after := r.SettleUntil(10*time.Second, answered)
	if n := len(approvals(after)); n != 0 {
		t.Errorf("the fault changed: %d approval items after the restart, the fault gives none", n)
	}
	lost := false
	for _, v := range enginetest.Restarted(st, after) {
		lost = lost || v.Rule == "restart-lost-item" && strings.HasPrefix(v.Detail, as[0].ID+" ")
	}
	if !lost {
		t.Errorf("the restart check does not report %s lost", as[0].ID)
	}
}
