package filefed

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// clineStart writes the turns as a Cline session of the last ten minutes
// and starts a rig over it.
func clineStart(t *testing.T, extra map[int]sessiongen.ClineTurn, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := support.NewEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.Home, &sessiongen.Cline{Extra: extra}, sessiongen.Script{Seed: 51, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	return l, e.Rig(t, "cline")
}

func clineStarter(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	return clineStart(t, nil, turns...)
}

// After a /new Cline writes a new session folder. Its manifest names no
// pid (the recorded 3.0.74 manifests have none), and no other Cline runs
// in the folder, so it is this pane's: the session follows it, keeping
// the first conversation's items, shows each prompt once, types a prompt
// from the phone once, and after a helper restart stays on it. Keeping
// both conversations is this agent's current convention; the question
// whether every agent should start over instead is open.
func TestClineFollowsNewConversation(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/lifecycle/new_conversation")
	e := support.NewEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.Home, &sessiongen.Cline{}, sessiongen.Script{Seed: 51, Start: e.Start, Cwd: e.Cwd,
		Turns: []sessiongen.Turn{{Say: 20, Tools: 1, Output: 20}, {Say: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	r := e.RigMod(t, "cline", func(o *enginetest.Options) { o.PanePIDs = support.ProcsOf(t) })
	support.SettleUntil(r, 2)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20}); err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d sessions", len(l.Mains))
	}
	st := support.SettleUntil(r, 3)
	support.Fail(t, "after /new", enginetest.Paging(st))
	const text = "Step 4: from the phone, in the new conversation"
	if _, err := r.Send(text, harness.SendNow); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := r.Term.Submitted(); len(got) != 1 || got[0] != text {
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
	r.Restart()
	after := support.SettleUntil(r, 4)
	if !support.HasPrompt(3)(after) || countPrompt(after, text) != 1 {
		t.Fatal("the restart did not restore the new conversation")
	}
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	more := support.SettleUntil(r, 5)
	if !support.HasPrompt(5)(more) {
		t.Fatal("the new conversation is not followed after the restart")
	}
	support.Fail(t, "after restart", enginetest.Paging(more))
}

// TestKnownFault_ClineDocumentOver8MiB documents a fault: Cline rewrites its
// whole messages document, and the helper reads a file source at most
// 8 MiB (source/file.go readCapped, ErrTooLarge). A conversation whose
// document passes that is never read again: the chat shows nothing of it.
//
// Correct: a long conversation still shows, at least its newest part. This
// test passes while the fault is present; when it fails the fault is fixed:
// delete it.
func TestKnownFault_ClineDocumentOver8MiB(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, 10*time.Minute)
	turns := make([]sessiongen.Turn, 40)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 20, Tools: 4, Output: 60_000}
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.Cline{}, sessiongen.Script{Seed: 3, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(l.Main()); err != nil || fi.Size() <= 8<<20 {
		t.Fatalf("document is %d bytes (%v): want above 8 MiB", fi.Size(), err)
	}
	r := e.Rig(t, "cline")
	if !support.Never(r, 4*time.Second, func(st enginetest.State) bool { return len(st.Items) > 0 }) {
		t.Fatal("the conversation shows: the fault is fixed")
	}
}

// Cline killed at work leaves a tool with no result and a question nobody
// answered. When the hub reports the agent exited they end and nothing
// waits for the user; relaunched, the same session carries on.
func TestClineExitAndRelaunch(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/lifecycle/agent_exits", "cline/lifecycle/relaunch", "cline/capability/needs.questions")
	extra := map[int]sessiongen.ClineTurn{
		2: {Question: &sessiongen.ClineQuestion{Question: "Which language?", Options: []string{"Go", "Rust"}, Open: true}},
	}
	l, r := clineStart(t, extra, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Tools: 1, Output: 20})
	r.Hub("working")
	st := support.SettleUntil(r, 2)
	if st.Mission.Summary.NeedsYou != 1 || !st.Mission.Summary.Working {
		t.Fatalf("before the exit: %+v, want one question waiting, working", st.Mission.Summary)
	}
	qs := support.Items(st, support.OfKind(harness.KindQuestion))
	if len(qs) != 1 || len(qs[0].Question.Answered) != 0 || len(qs[0].Question.Questions[0].Options) != 2 {
		t.Fatalf("questions %+v, want one open with two options", qs)
	}
	r.Hub("ended")
	l.Truth.Exited = true
	st = support.WaitFor(t, r, "the exit", func(st enginetest.State) bool {
		return !st.Mission.Summary.Working && st.Mission.Summary.NeedsYou == 0
	})
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		if len(it.Question.Answered) == 0 && (it.Question.Answerable == nil || *it.Question.Answerable) {
			t.Errorf("question %s still open after the exit", it.ID)
		}
	}
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		if it.Tool.State == harness.StateRunning {
			t.Errorf("tool %s still running after the exit", it.ID)
		}
	}
	if n := len(support.TurnsIn(st, harness.StateRunning)); n != 0 {
		t.Errorf("%d turns running after the exit", n)
	}
	support.Fail(t, "after the exit", enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"))
	l.Truth.Exited = false
	r.Hub("working")
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20, Tools: 1, Output: 20}); err != nil {
		t.Fatal(err)
	}
	r.Hub("idle") // Cline's file has no turn state: the hub says when it is done
	after := support.SettleUntil(r, 3)
	support.CheckEdge(t, "after the relaunch", after, l)
	r.Restart()
	again := support.SettleUntil(r, 3)
	support.CheckEdge(t, "restart", again, l)
	support.Fail(t, "restart", support.OfferedAsks(after, support.ExitNotices(support.Restarted(after, again))))
}

// A failed run ends its turn failed with the reason in words; the next
// turn works; a restart reads the same.
func TestClineFailedTurn(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/lifecycle/turn_end")
	l, r := clineStart(t, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Error: true}, sessiongen.Turn{Say: 20})
	st := support.SettleUntil(r, 3)
	support.CheckEdge(t, "failed turn", st, l)
	if n := len(support.TurnsIn(st, harness.StateFailed)); n != 1 {
		t.Errorf("%d failed turns, want 1", n)
	}
	if n := len(support.TurnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns, want 2", n)
	}
	errs := support.Items(st, support.Notice(harness.NoticeError))
	if len(errs) != 1 || !strings.Contains(errs[0].Notice.Text, "API key") {
		t.Errorf("error notices %+v: want one that says the key was refused", errs)
	}
	if st.Mission.Summary.Working {
		t.Error("working after the last turn ended")
	}
	r.Restart()
	after := support.SettleUntil(r, 3)
	support.CheckEdge(t, "restart", after, l)
	support.Fail(t, "restart", support.Restarted(st, after))
}

// What Cline's document shows of its capabilities: tool steps with their
// results, edits, commands, thinking, questions answered and cancelled, the
// mode of a prompt; paged history reads the same whatever the page size.
func TestClineCapabilitiesInFiles(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/capability/chat", "cline/capability/needs.questions")
	extra := map[int]sessiongen.ClineTurn{
		1: {Edits: 2, Commands: 1, Think: true, Mode: "plan"},
		2: {Question: &sessiongen.ClineQuestion{Question: "Which language?", Options: []string{"Go", "Rust"}, Answer: "Rust"}},
		3: {Question: &sessiongen.ClineQuestion{Question: "Which editor?", Options: []string{"vi", "ed"}, Cancelled: true}},
	}
	l, r := clineStart(t, extra, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20})
	st := support.SettleUntil(r, 3)
	support.CheckEdge(t, "files", st, l)
	qs := map[string][]string{}
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		qs[it.Question.Questions[0].Prompt] = it.Question.Answered["0"]
	}
	if a, ok := qs["Which language?"]; !ok || strings.Join(a, "+") != "Rust" {
		t.Errorf("answered %v, want Rust", qs)
	}
	if a, ok := qs["Which editor?"]; !ok || a == nil || len(a) != 0 {
		t.Errorf("cancelled question %v, want closed with no answer", qs)
	}
	var edits, shells int
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		if it.Tool.State != harness.StateOK {
			t.Errorf("tool %s %s", it.ID, it.Tool.State)
		}
		switch it.Tool.Category {
		case harness.ToolWrite, harness.ToolEdit:
			edits++
			if it.Tool.Subject == nil || !strings.Contains(it.Tool.Subject.Text, "e1_") {
				t.Errorf("edit %+v: want the file as its subject", it.Tool)
			}
		case harness.ToolShell:
			shells++
		}
	}
	if edits != 2 || shells != 2 {
		t.Errorf("%d edits and %d commands, want 2 and 2", edits, shells)
	}
	if n := len(support.Items(st, support.OfKind(harness.KindThinking))); n != 1 {
		t.Errorf("%d thinking items, want 1", n)
	}
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("needs you %d with every question answered", st.Mission.Summary.NeedsYou)
	}
	for _, size := range []int{1, 4, 9} {
		support.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(size)))
		support.Fail(t, "paging", enginetest.Paging(r.ReadPages(size)))
	}
}

func TestClineConnectionDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/lifecycle/connection_drop")
	support.DropAndReturn(t, clineStarter)
}

func TestClineSessionRemoved(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/lifecycle/session_removed")
	support.RemoveAndServeAgain(t, clineStarter)
}

func TestClineConcurrentReaders(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/scale/concurrent_requests")
	support.ConcurrentReaders(t, clineStarter)
}

// TestKnownFault_ClineKilledTurnEndsOK documents the fault of
// TestKnownFault_AiderKilledTurnEndsOK for Cline: when the hub reports the
// agent exited at work, the working flag falls first and the mapper closes
// the open turn as the pane going idle would (engine/sess.go statusChanged,
// adapters/cline/messages.go CloseIdle), so a turn killed with a tool
// running ends ok. Its tool does end interrupted.
//
// Correct: a turn open when the agent exits ends interrupted. This test
// passes while the fault is present; when it fails the fault is fixed:
// delete it.
func TestKnownFault_ClineKilledTurnEndsOK(t *testing.T) {
	t.Parallel()
	_, r := clineStart(t, map[int]sessiongen.ClineTurn{1: {Unfinished: true}}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := support.SettleUntil(r, 1)
	if len(support.TurnsIn(st, harness.StateRunning)) != 1 {
		t.Fatal("no turn at work before the exit")
	}
	r.Hub("ended")
	st = support.WaitFor(t, r, "the exit", func(st enginetest.State) bool { return len(support.TurnsIn(st, harness.StateRunning)) == 0 })
	if len(support.TurnsIn(st, harness.StateInterrupted)) != 0 || len(support.TurnsIn(st, harness.StateOK)) != 1 {
		t.Fatalf("the killed turn: %d ok, %d interrupted: the fault is fixed", len(support.TurnsIn(st, harness.StateOK)), len(support.TurnsIn(st, harness.StateInterrupted)))
	}
}
