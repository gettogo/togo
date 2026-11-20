package generic

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestKnownFault_QueuedPromptJudgedByThePreviousTurnsEnd documents a
// fault in the engine's check that a prompt from the phone got a reply
// (engine/sess.go afterTurnLocked, replyMissingLocked). The check is armed
// when a turn ends and runs turnSettle (6 s) later. A prompt queued behind
// that turn is typed as it ends (engine/typist_queue.go), which sets
// awaitReply, so the check armed by the previous turn judges the queued
// prompt:
//
//   - typed before the check runs, and the agent not yet seen working,
//     the chat says "omp ended the turn without a reply" although the turn
//     that prompt starts has not ended; the agent's reply then follows the
//     error notice;
//   - typed after the check ran, nothing judges it: a prompt the agent
//     never answers gets no notice at all.
//
// Which one happens depends on how long typing took, so the tape of
// send-while-working differed between two runs on a loaded runner
// (enginetest/tape determinism_test.go: one export had the notice, the
// other not). Here the engine's waits are scaled up (turnSettle 3 s) so
// the prompt is always typed first.
//
// Correct: the check for a typed prompt runs after a turn that began
// after it, so an answered prompt never gets the notice and an unanswered
// one always does.
//
// Trigger: a prompt queued from the phone while the agent works, when the
// agent takes longer than turnSettle after the earlier turn ended to show
// it is working on the queued prompt.
func TestKnownFault_QueuedPromptJudgedByThePreviousTurnsEnd(t *testing.T) {
	t.Parallel()
	lv := start(t, "omp", func(o *enginetest.Options) { o.Scale = 0.5 })
	r := lv.r
	const text = "Queued while the agent works"
	if _, err := r.Send(text, harness.SendQueue); err != nil {
		t.Fatalf("queue: %v", err)
	}
	lv.idle()
	eventually(t, 30*time.Second, "the queued prompt to be typed", func() bool { return slices.Contains(r.Term.Submitted(), text) })
	noReply := func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.Notice != nil && strings.HasPrefix(it.ID, "noreply-") {
				return true
			}
		}
		return false
	}
	if !noReply(r.SettleUntil(30*time.Second, noReply)) {
		t.Fatal("the fault is fixed: the queued prompt got no notice before its turn; delete this test")
	}
	// The agent answers the queued prompt.
	if err := lv.l.Append(sessiongen.Turn{Prompt: text, Say: 20}); err != nil {
		t.Fatal(err)
	}
	st := r.SettleUntil(30*time.Second, enginetest.Has(lv.l.Truth.Last))
	if !enginetest.Has(lv.l.Truth.Last)(st) {
		t.Fatal("the agent's reply to the queued prompt is not shown")
	}
	if !noReply(st) {
		t.Error("the fault changed: the notice is gone after the reply")
	}
}
