package hookfed

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestAskOfferAnsweredWhileOpeningIsNotDrawn: the engine offers an open
// question to the phone as an ask of its own after offerGrace
// (engine/feedapproval.go). When the question is answered at the desk
// while that offer opens (the open event and OpenAsk's return held late by
// the rig, HoldApprovalOpens and GateAsks), the offer is dropped: no
// approval card is drawn on its own, nothing stays pending, and a restart
// changes nothing. Before the fix the ask was drawn as an ownerless card
// resolved at the desk, which a restart then lost: the "restart-lost-item
// approval-r_..." TestCopilotRestartsAtSeededPoints reported on a loaded
// runner.
func TestAskOfferAnsweredWhileOpeningIsNotDrawn(t *testing.T) {
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
	releaseAsk()
	approvals := func(st enginetest.State) []harness.Item {
		return support.Items(st, support.OfKind(harness.KindApproval))
	}
	st := r.SettleUntil(30*time.Second, func(enginetest.State) bool { return len(r.Pending()) == 0 })
	if p := r.Pending(); len(p) != 0 {
		t.Fatalf("%d approvals still pending after the question was answered", len(p))
	}
	for _, it := range approvals(st) {
		if it.Approval.Owner == "" {
			t.Errorf("approval %s %+v is drawn as a card of its own for an answered question", it.ID, *it.Approval)
		}
	}
	r.Restart()
	after := r.SettleUntil(10*time.Second, answered)
	enginetest.Fail(t, "restart", enginetest.Restarted(st, after))
}
