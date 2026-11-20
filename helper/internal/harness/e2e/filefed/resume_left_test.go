package filefed

import (
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// resumeToLeft drives a /new and then a resume of the first conversation
// (the layout's extra turn 3 resumes file 1), on a rig whose first turn is
// written. The session follows the resumed file: its new prompt shows, no
// item is shown twice, a prompt sent from the phone right after is typed
// once and shown once, and a helper restart stays on the resumed file.
func resumeToLeft(t *testing.T, l *sessiongen.Layout, r *enginetest.Rig) {
	t.Helper()
	support.SettleUntil(r, 1)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20}); err != nil {
		t.Fatal(err)
	}
	support.SettleUntil(r, 2)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil { // resumed: the first file again
		t.Fatal(err)
	}
	st := support.SettleUntil(r, 3)
	support.CheckEdge(t, "after resume", st, l)
	support.Fail(t, "after resume", enginetest.Paging(st))
	const text = "Step 4: from the phone, after the resume"
	if _, err := r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := r.Term.Submitted(); !slices.Equal(got, []string{text}) {
		t.Fatalf("the agent was given %q", got)
	}
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Prompt: text, Say: 20}); err != nil {
		t.Fatal(err)
	}
	st = support.SettleUntil(r, 4)
	if n := countPrompt(st, text); n != 1 {
		t.Fatalf("the phone's prompt is shown %d times, want 1", n)
	}
	support.Fail(t, "phone prompt", enginetest.Paging(st))
	// A restart reads the file the pane is on: the resumed conversation,
	// each of its prompts once.
	r.Restart()
	after := support.SettleUntil(r, 4)
	for _, n := range []int{1, 3, 4} {
		if !support.HasPrompt(n)(after) {
			t.Fatalf("prompt %d of the resumed conversation is missing after the restart", n)
		}
	}
	if n := countPrompt(after, text); n != 1 {
		t.Fatalf("the phone's prompt is shown %d times after the restart, want 1", n)
	}
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil { // still the resumed file
		t.Fatal(err)
	}
	more := support.SettleUntil(r, 5)
	support.CheckEdge(t, "after restart", more, l)
	support.Fail(t, "after restart", enginetest.Paging(more))
}

func countPrompt(st enginetest.State, text string) int {
	n := 0
	for _, it := range support.UserPrompts(st) {
		if it.User.Text == text {
			n++
		}
	}
	return n
}
