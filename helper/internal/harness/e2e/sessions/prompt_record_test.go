package sessions

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// promptCount is how many prompts the chat shows with this text.
func promptCount(r *enginetest.Rig, text string) int {
	n := 0
	for _, it := range support.UserPrompts(r.Read()) {
		if it.User.Text == text {
			n++
		}
	}
	return n
}

// recordAfterSent is an agent that writes a submitted prompt to its file
// once the send has returned and the chat shows the prompt: what a real
// agent does when the helper's confirmation is quicker than its write. It
// is the order the send tests assert on; the other order is
// TestPromptRecordedBeforeSendReturnsShowsOnce.
func recordAfterSent(t *testing.T, r *enginetest.Rig, record func(text string) error) (hook func(string), written <-chan struct{}) {
	done := make(chan struct{})
	return func(text string) {
		go func() {
			defer close(done)
			for deadline := time.Now().Add(support.DriveLimit); promptCount(r, text) == 0; time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Errorf("the chat never showed %q", text)
					return
				}
			}
			if err := record(text); err != nil {
				t.Error(err)
			}
		}()
	}, done
}

// A prompt the engine typed is remembered before the send returns, so an
// agent whose own record of it is read while the send still waits on the
// screen is matched to the phone's prompt: the chat shows it once.
func TestPromptRecordedBeforeSendReturnsShowsOnce(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/chat")
	a := newOMPAgent(t, nil, sessiongen.Turn{Say: 30, Tools: 1, Output: 40})
	r := a.r
	r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	const text = "Carry on, recorded before the send returns"
	r.Term.OnSubmit = func(text string) {
		if err := a.l.Append(sessiongen.Turn{Prompt: text, Say: 20}); err != nil {
			t.Error(err)
			return
		}
		// Hold the key press until the engine has read the agent's record.
		for deadline := time.Now().Add(support.DriveLimit); promptCount(r, text) == 0; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Errorf("the engine never read the agent's record of %q", text)
				return
			}
		}
	}
	if _, err := r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	st := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	n := 0
	for _, it := range support.UserPrompts(st) {
		if it.User.Text == text {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the chat shows the prompt %d times, want once", n)
	}
}
