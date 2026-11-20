package filefed

import (
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// withProcs is a with a process view over the rig's pane, as a real pane
// has: an agent whose files or rows name no process is followed to
// another conversation only when no other agent of its kind runs in the
// folder.
func withProcs(t *testing.T, a fileAgent) fileAgent {
	pids := support.ProcsOf(t)
	prev := a.opts
	a.opts = func(o *enginetest.Options) {
		if prev != nil {
			prev(o)
		}
		o.PanePIDs = pids
	}
	return a
}

// followsConversations drives an agent that starts a new conversation by
// itself (a /new, or started again without continuing) and then resumes
// the first. Each conversation the agent is on is followed and nothing is
// shown twice; a prompt sent from the phone right after the move is typed
// once and shown once; the resumed conversation's later messages show;
// a helper restart stays on the conversation the agent is on.
func followsConversations(t *testing.T, a fileAgent, seed int64) {
	t.Helper()
	a = withProcs(t, a)
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: seed})
	a.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	a.seen(t, r, "first conversation", l, false)
	// From here the agent writes at the present: a file is taken only
	// when it was written since the feed began to watch.
	add := func(turns ...sessiongen.Turn) {
		support.Now(l)
		a.add(t, l, turns...)
	}
	// The move is seen while its first turn runs, as a real turn takes
	// longer than the helper's look (a Vibe journal found only after its
	// turn ended shows from the next prompt:
	// TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing).
	add(open(sessiongen.Turn{New: true, Say: 30, Tools: 1, Output: 50}))
	a.seen(t, r, "new conversation, mid turn", l, true)
	add(sessiongen.Turn{Prompt: a.cont, Say: 30})
	st := a.seen(t, r, "new conversation", l, false)
	support.Fail(t, "new conversation", enginetest.Paging(st))
	const text = "From the phone, in the new conversation"
	if _, err := r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := r.Term.Submitted(); !slices.Equal(got, []string{text}) {
		t.Fatalf("the agent was given %q", got)
	}
	add(sessiongen.Turn{Prompt: text, Say: 30})
	st = a.seen(t, r, "phone prompt", l, false)
	if n := countPrompt(st, text); n != 1 {
		t.Fatalf("the phone's prompt is shown %d times, want 1", n)
	}
	support.Fail(t, "phone prompt", enginetest.Paging(st))
	add(open(sessiongen.Turn{Say: 30, Tools: 1, Output: 50, Hook: &sessiongen.HookTurn{Resume: true}}))
	a.seen(t, r, "resumed, mid turn", l, true)
	add(sessiongen.Turn{Prompt: a.cont, Say: 30})
	st = a.seen(t, r, "resumed", l, false)
	support.Fail(t, "resumed", enginetest.Paging(st))
	// The restart falls inside a turn of the resumed conversation (one
	// between Vibe's turns shows nothing until the next prompt:
	// TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing).
	add(open(sessiongen.Turn{Say: 30, Tools: 1, Output: 50}))
	a.seen(t, r, "before restart", l, true)
	r.Restart()
	a.seen(t, r, "after restart", l, true)
	add(sessiongen.Turn{Prompt: a.cont, Say: 30})
	st = a.seen(t, r, "resumed after restart", l, false)
	support.Fail(t, "after restart", enginetest.Paging(st))
}
