package generic

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestQueuedPromptJudgedAfterItsOwnTurn: the engine's check that a
// prompt from the phone got a reply (engine/sess.go afterTurnLocked,
// replyMissingLocked) is armed when a turn ends. A prompt queued behind
// that turn is typed as it ends; it is judged after a turn of its own,
// not by the check the earlier turn armed. With the engine's waits scaled
// up (turnSettle 3 s), no "ended the turn without a reply" notice comes
// while the agent has not started on the queued prompt, and none after
// it answers. Before the fix the earlier turn's check judged it, so the
// notice came before the agent's reply, or never, by how long typing
// took; the send-while-working tape differed between runs by it.
func TestQueuedPromptJudgedAfterItsOwnTurn(t *testing.T) {
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
	// Longer than the earlier turn's check (turnSettle, 3 s here).
	if !support.Never(r, 4*time.Second, noReply) {
		t.Fatal("the queued prompt got a no-reply notice before its own turn")
	}
	// The agent answers the queued prompt.
	if err := lv.l.Append(sessiongen.Turn{Prompt: text, Say: 20}); err != nil {
		t.Fatal(err)
	}
	st := r.SettleUntil(30*time.Second, enginetest.Has(lv.l.Truth.Last))
	if noReply(st) {
		t.Error("the answered queued prompt has a no-reply notice")
	}
}
