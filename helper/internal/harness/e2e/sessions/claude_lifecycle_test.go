package sessions

import (
	"strconv"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

func ids(st enginetest.State) map[string]bool {
	out := map[string]bool{}
	for _, it := range st.Items {
		out[it.ID] = true
	}
	return out
}

// TestKnownFault_ClaudeNewConversationIsNotFollowed documents a fault:
// /clear (and /resume) in Claude Code starts another transcript and says
// so with SessionStart, but the chat does not start over. The engine
// starts a chat over when a mapper names a new conversation in its state
// "conversation" (engine/conversation.go StateConversation); Claude Code's
// transcript and hook mappers set only harness_session
// (adapters/claude/transcript.go:160, hooks.go:63). The old
// conversation's items stay, the log keeps its epoch, and the new
// conversation's items follow them. A prompt sent from the phone still
// reaches Claude.
//
// Correct: the chat starts over on the new conversation, as omp's does
// (adapters/omp/session.go header). This test passes while the fault is
// present; when it fails the fault is fixed: delete it and claim
// claude/lifecycle/new_conversation and claude/lifecycle/resume.
func TestKnownFault_ClaudeNewConversationIsNotFollowed(t *testing.T) {
	t.Parallel()
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{},
		sessiongen.Turn{Say: 30, Tools: 1, Output: 40}, sessiongen.Turn{Say: 30})
	first := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	firstFile := l.Main()
	oldIDs := ids(first)
	if err := l.Append(sessiongen.Turn{New: true, Say: 30, Tools: 1, Output: 40}); err != nil {
		t.Fatal(err)
	}
	claudeStart(r, l.Main(), "clear")
	second := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	if second.Epoch != first.Epoch {
		t.Fatal("the fault is fixed: the chat started over after /clear")
	}
	kept := 0
	for _, it := range second.Items {
		if oldIDs[it.ID] {
			kept++
		}
	}
	if kept != len(first.Items) || len(support.UserPrompts(second)) != 3 {
		t.Errorf("%d of the old items kept, %d prompts: the fault changed", kept, len(support.UserPrompts(second)))
	}
	hook, written := recordAfterSent(t, r, func(text string) error {
		return l.Append(sessiongen.Turn{Prompt: text, Say: 20})
	})
	r.Term.OnSubmit = hook
	if _, err := r.Send("Carry on here", harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	<-written
	st := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	if got := r.Term.Submitted(); len(got) != 1 || got[0] != "Carry on here" || len(support.UserPrompts(st)) != 4 {
		t.Errorf("claude was given %q; %d prompts", got, len(support.UserPrompts(st)))
	}
	claudeStart(r, firstFile, "resume")
	back := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	if back.Epoch != first.Epoch || len(back.Items) < len(st.Items) {
		t.Errorf("after /resume the epoch is %s (was %s) and %d items (were %d): the fault changed", back.Epoch, first.Epoch, len(back.Items), len(st.Items))
	}
}

// claudeByRecords is a script whose transcript holds exactly n records:
// turns of a prompt, k tool calls with their results, a reply and the
// turn's end are 3+2k records each.
func claudeByRecords(n int) []sessiongen.Turn {
	turns := n / 11
	if turns%2 != n%2 {
		turns--
	}
	tools := (n - 3*turns) / 2
	out := make([]sessiongen.Turn, turns)
	for i := range out {
		k := tools / turns
		if i < tools%turns {
			k++
		}
		out[i] = sessiongen.Turn{Say: 40, Tools: k, Output: 100}
	}
	return out
}

// The restore limits for a Claude Code transcript: 2,000 records and 8 MiB,
// each below and at the limit. The restored chat holds every rule, and so
// does the one after a helper restart.
func TestClaudeRestoreLimits(t *testing.T) {
	t.Parallel()
	// The subtests run in this test's turn, not in parallel. This test keeps
	// its Heavy place until they finish; a parallel subtest waits for a
	// parallel slot, and on a small machine tests waiting for a Heavy place
	// hold every slot, so neither side could go on.
	support.Heavy(t)
	coverage.Covers(t, "generated", "claude/scale/at_limit")
	for _, c := range []struct {
		name string
		n    int
	}{{"records_below", maxRecords - 1}, {"records_at", maxRecords}} {
		t.Run(c.name, func(t *testing.T) {
			_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{MaxItems: 20000}, claudeByRecords(c.n)...)
			if got := l.Records[l.Main()]; got != c.n {
				t.Fatalf("transcript holds %d records, want %d", got, c.n)
			}
			restoreClaude(t, l, r)
		})
	}
	t.Run("size", func(t *testing.T) {
		for _, size := range []int{maxFileRead - 1024, maxFileRead} {
			t.Run(strconv.Itoa(size), func(t *testing.T) {
				e := support.NewEnv(t, time.Hour)
				turns := func(last int) []sessiongen.Turn {
					var out []sessiongen.Turn
					for i := 0; i < size/(4*60_000); i++ {
						out = append(out, sessiongen.Turn{Say: 40, Tools: 4, Output: 60_000})
					}
					return append(out, sessiongen.Turn{Say: 40, Tools: 1, Output: last})
				}
				// One trial write tells the size; the last output makes it exact.
				l, err := sessiongen.Write(e.Home, &sessiongen.Claude{}, sessiongen.Script{Seed: 63, Start: e.Start, Cwd: e.Cwd, Turns: turns(1000)})
				if err != nil {
					t.Fatal(err)
				}
				fi := fileSize(t, l.Main())
				e = support.NewEnv(t, time.Hour)
				l, err = sessiongen.Write(e.Home, &sessiongen.Claude{}, sessiongen.Script{Seed: 63, Start: e.Start, Cwd: e.Cwd, Turns: turns(1000 + size - fi)})
				if err != nil {
					t.Fatal(err)
				}
				if got := fileSize(t, l.Main()); got != size {
					t.Fatalf("transcript is %d bytes, want %d", got, size)
				}
				r := e.RigWith(t, enginetest.Options{Agent: "claude", NoRedact: true, MaxItems: 20000})
				r.Term.Show(r.Term.MustCapture(claudeIdle))
				claudeStart(r, l.Main(), "startup")
				restoreClaude(t, l, r)
			})
		}
	})
}

// restoreClaude checks the restored chat, and that a helper restart finds
// the same transcript again and shows the same chat.
func restoreClaude(t *testing.T, l *sessiongen.Layout, r *enginetest.Rig) {
	t.Helper()
	st := r.SettleUntil(40*time.Second, enginetest.Has(l.Truth.Last))
	support.Check(t, "restored", st, l)
	r.Restart()
	after := r.SettleUntil(40*time.Second, enginetest.Has(l.Truth.Last))
	support.Check(t, "after restart", after, l)
	support.Fail(t, "restart", support.Restarted(st, after), l)
}

// A Claude Code session of many turns (tool calls of 10 KB, a background
// job every 20 turns, a compaction),
// written in pieces while the engine reads it, with a helper restart after
// each piece but the last. The default size crosses the record limit; at
// TOGO_E2E_LONG=1 the transcript is over 8 MiB too. A restart finds the
// transcript again with no hook, and every rule holds throughout.
func TestClaudeLongSession(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "claude/scale/long_session")
	n := support.Scale(190, 250)
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 300, Tools: 4, Output: support.Scale(300, 10_000)}
		if i%20 == 7 {
			turns[i].Jobs = 1
		}
		if i == n/2 {
			turns[i].Compact = true
		}
	}
	_, l, r := claudeSession(t, 48*time.Hour, nil, enginetest.Options{MaxItems: 20000}, turns[:3]...)
	points := []int{3}
	points = append(points, support.Cuts(64, support.Scale(2, 4), 4, n-1)...)
	st := support.Drive(t, r, l, turns, points)
	if l.Records[l.Main()] <= maxRecords {
		t.Errorf("%d records: the transcript must cross the restore limit of %d", l.Records[l.Main()], maxRecords)
	}
	if len(support.UserPrompts(st)) != n || len(st.Pages) < 2 {
		t.Errorf("%d prompts over %d pages, want %d over several", len(support.UserPrompts(st)), len(st.Pages), n)
	}
	support.RestoreTwice(t, r, l, n)
}
