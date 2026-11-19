package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// clineStart writes the turns as a Cline session of the last ten minutes
// and starts a rig over it.
func clineStart(t *testing.T, extra map[int]sessiongen.ClineTurn, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := newEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.home, &sessiongen.Cline{Extra: extra}, sessiongen.Script{Seed: 51, Start: e.start, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	return l, e.rig(t, "cline")
}

func clineStarter(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	return clineStart(t, nil, turns...)
}

// TestKnownFault_ClineNewConversationIsNeverFollowed documents a fault:
// after a /new Cline writes a new session folder, and the mapper follows
// it only when the new session's manifest names a pid that is in this
// session's process tree (adapters/cline/messages.go owner, Map); with no
// pid it keeps the old session. The recorded manifests
// (adapters/cline/testdata/session.json, ask.json) hold no pid, so the
// chat stays on the first conversation and never shows the second.
//
// Correct: a new session of the folder, started since the old one and
// written while no other Cline runs there, is followed. This test passes
// while the fault is present; when it fails the fault is fixed: delete it.
func TestKnownFault_ClineNewConversationIsNeverFollowed(t *testing.T) {
	l, r := clineStart(t, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Say: 20})
	settleUntil(r, 2)
	now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20}, sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d sessions", len(l.Mains))
	}
	if !never(r, 4500*time.Millisecond, hasPrompt(3)) {
		t.Fatal("the new conversation is followed: the fault is fixed")
	}
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
	e := newEnv(t, 10*time.Minute)
	turns := make([]sessiongen.Turn, 40)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 20, Tools: 4, Output: 60_000}
	}
	l, err := sessiongen.Write(e.home, &sessiongen.Cline{}, sessiongen.Script{Seed: 3, Start: e.start, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(l.Main()); err != nil || fi.Size() <= 8<<20 {
		t.Fatalf("document is %d bytes (%v): want above 8 MiB", fi.Size(), err)
	}
	r := e.rig(t, "cline")
	if !never(r, 4*time.Second, func(st enginetest.State) bool { return len(st.Items) > 0 }) {
		t.Fatal("the conversation shows: the fault is fixed")
	}
}

// Cline killed at work leaves a tool with no result and a question nobody
// answered. When the hub reports the agent exited they end and nothing
// waits for the user; relaunched, the same session carries on.
func TestClineExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "cline/lifecycle/agent_exits", "cline/lifecycle/relaunch", "cline/capability/needs.questions")
	extra := map[int]sessiongen.ClineTurn{
		2: {Question: &sessiongen.ClineQuestion{Question: "Which language?", Options: []string{"Go", "Rust"}, Open: true}},
	}
	l, r := clineStart(t, extra, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Tools: 1, Output: 20})
	r.Hub("working")
	st := settleUntil(r, 2)
	if st.Mission.Summary.NeedsYou != 1 || !st.Mission.Summary.Working {
		t.Fatalf("before the exit: %+v, want one question waiting, working", st.Mission.Summary)
	}
	qs := items(st, ofKind(harness.KindQuestion))
	if len(qs) != 1 || len(qs[0].Question.Answered) != 0 || len(qs[0].Question.Questions[0].Options) != 2 {
		t.Fatalf("questions %+v, want one open with two options", qs)
	}
	r.Hub("ended")
	l.Truth.Exited = true
	st = waitFor(t, r, "the exit", func(st enginetest.State) bool {
		return !st.Mission.Summary.Working && st.Mission.Summary.NeedsYou == 0
	})
	for _, it := range items(st, ofKind(harness.KindQuestion)) {
		if len(it.Question.Answered) == 0 && (it.Question.Answerable == nil || *it.Question.Answerable) {
			t.Errorf("question %s still open after the exit", it.ID)
		}
	}
	for _, it := range items(st, ofKind(harness.KindTool)) {
		if it.Tool.State == harness.StateRunning {
			t.Errorf("tool %s still running after the exit", it.ID)
		}
	}
	if n := len(turnsIn(st, harness.StateRunning)); n != 0 {
		t.Errorf("%d turns running after the exit", n)
	}
	fail(t, "after the exit", enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"))
	l.Truth.Exited = false
	r.Hub("working")
	now(l)
	if err := l.Append(sessiongen.Turn{Say: 20, Tools: 1, Output: 20}); err != nil {
		t.Fatal(err)
	}
	r.Hub("idle") // Cline's file has no turn state: the hub says when it is done
	after := settleUntil(r, 3)
	checkEdge(t, "after the relaunch", after, l)
	r.Restart()
	again := settleUntil(r, 3)
	checkEdge(t, "restart", again, l)
	fail(t, "restart", exitNotices(restarted(after, again)))
}

// A failed run ends its turn failed with the reason in words; the next
// turn works; a restart reads the same.
func TestClineFailedTurn(t *testing.T) {
	coverage.Covers(t, "generated", "cline/lifecycle/turn_end")
	l, r := clineStart(t, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Error: true}, sessiongen.Turn{Say: 20})
	st := settleUntil(r, 3)
	checkEdge(t, "failed turn", st, l)
	if n := len(turnsIn(st, harness.StateFailed)); n != 1 {
		t.Errorf("%d failed turns, want 1", n)
	}
	if n := len(turnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns, want 2", n)
	}
	errs := items(st, notice(harness.NoticeError))
	if len(errs) != 1 || !strings.Contains(errs[0].Notice.Text, "API key") {
		t.Errorf("error notices %+v: want one that says the key was refused", errs)
	}
	if st.Mission.Summary.Working {
		t.Error("working after the last turn ended")
	}
	r.Restart()
	after := settleUntil(r, 3)
	checkEdge(t, "restart", after, l)
	fail(t, "restart", restarted(st, after))
}

// What Cline's document shows of its capabilities: tool steps with their
// results, edits, commands, thinking, questions answered and cancelled, the
// mode of a prompt; paged history reads the same whatever the page size.
func TestClineCapabilitiesInFiles(t *testing.T) {
	coverage.Covers(t, "generated", "cline/capability/chat", "cline/capability/needs.questions")
	extra := map[int]sessiongen.ClineTurn{
		1: {Edits: 2, Commands: 1, Think: true, Mode: "plan"},
		2: {Question: &sessiongen.ClineQuestion{Question: "Which language?", Options: []string{"Go", "Rust"}, Answer: "Rust"}},
		3: {Question: &sessiongen.ClineQuestion{Question: "Which editor?", Options: []string{"vi", "ed"}, Cancelled: true}},
	}
	l, r := clineStart(t, extra, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20})
	st := settleUntil(r, 3)
	checkEdge(t, "files", st, l)
	qs := map[string][]string{}
	for _, it := range items(st, ofKind(harness.KindQuestion)) {
		qs[it.Question.Questions[0].Prompt] = it.Question.Answered["0"]
	}
	if a, ok := qs["Which language?"]; !ok || strings.Join(a, "+") != "Rust" {
		t.Errorf("answered %v, want Rust", qs)
	}
	if a, ok := qs["Which editor?"]; !ok || a == nil || len(a) != 0 {
		t.Errorf("cancelled question %v, want closed with no answer", qs)
	}
	var edits, shells int
	for _, it := range items(st, ofKind(harness.KindTool)) {
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
	if n := len(items(st, ofKind(harness.KindThinking))); n != 1 {
		t.Errorf("%d thinking items, want 1", n)
	}
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("needs you %d with every question answered", st.Mission.Summary.NeedsYou)
	}
	for _, size := range []int{1, 4, 9} {
		fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(size)))
		fail(t, "paging", enginetest.Paging(r.ReadPages(size)))
	}
}

func TestClineConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "cline/lifecycle/connection_drop")
	dropAndReturn(t, clineStarter)
}

func TestClineSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "cline/lifecycle/session_removed")
	removeAndServeAgain(t, clineStarter)
}

func TestClineConcurrentReaders(t *testing.T) {
	coverage.Covers(t, "generated", "cline/scale/concurrent_requests")
	concurrentReaders(t, clineStarter)
}
