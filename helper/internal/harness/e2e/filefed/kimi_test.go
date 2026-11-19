package filefed

import (
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// kimiStart writes the turns as a Kimi session and starts a rig over it.
// With a process view the engine follows a /new, but it takes a first
// file only when it is written after the engine started (the file system's
// birth time is not portable), so the rig starts first.
func kimiStart(t *testing.T, procs bool, extra map[int]sessiongen.KimiTurn, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := kimiEnv(t, 10*time.Minute)
	var r *enginetest.Rig
	if procs {
		e.Start = time.Now().Add(-time.Second).UTC().Truncate(time.Second)
		r = e.RigMod(t, "kimi", func(o *enginetest.Options) { o.PanePIDs = support.ProcsOf(t) })
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.Kimi{Extra: extra}, sessiongen.Script{Seed: 41, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		r = e.Rig(t, "kimi")
	}
	return l, r
}

func kimiStarter(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	return kimiStart(t, false, nil, turns...)
}

func openQuestions(st enginetest.State) map[string][]string {
	out := map[string][]string{}
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		out[it.ID] = nil
		// Closed: answered, or withdrawn because its agent exited.
		if len(it.Question.Answered) > 0 || it.Question.Answerable != nil && !*it.Question.Answerable {
			out[it.ID] = []string{}
		}
		for _, a := range it.Question.Answered {
			out[it.ID] = append(out[it.ID], a...)
		}
	}
	return out
}

// /new in Kimi starts another session folder with its own log. The
// session moves to it, starts the chat over on it, and follows the
// new log after a helper restart.
func TestKimiNewConversation(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/new_conversation", "kimi/lifecycle/helper_restart")
	l, r := kimiStart(t, true, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 30}, sessiongen.Turn{Say: 20})
	support.CheckEdge(t, "first conversation", support.SettleUntil(r, 2), l)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20, Tools: 1, Output: 30}, sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d logs", len(l.Mains))
	}
	st := support.SettleUntil(r, 2)
	support.CheckEdge(t, "after /new", st, l)
	if n := len(support.UserPrompts(st)); n != 2 {
		t.Fatalf("%d prompts after /new, want 2 (the new conversation alone)", n)
	}
	r.Restart()
	after := support.SettleUntil(r, 2)
	support.CheckEdge(t, "after restart", after, l)
	support.Fail(t, "restart", support.Restarted(st, after))
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	more := support.SettleUntil(r, 3)
	support.CheckEdge(t, "new conversation after restart", more, l)
	support.Fail(t, "paging", enginetest.Paging(more))
}

// Kimi Code appends and closes its file per entry. After a /new, a resume of
// the first conversation appends to the file the session left: the
// session follows it back (resumeToLeft).
func TestKimiResumeToLeftConversation(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/resume")
	l, r := kimiStart(t, true, map[int]sessiongen.KimiTurn{3: {Resume: 1}}, sessiongen.Turn{Say: 20})
	resumeToLeft(t, l, r)
}

// TestKnownFault_KimiReplayWindowLeavesItemsWithoutATurn documents the
// fault of TestKnownFault_PiReplayWindowStartsATurnMidTurn for Kimi: a
// restart replays the last 2000 records of a long log (source/file.go
// MaxRecords, cut at any record). When the window begins inside a turn,
// the records of that turn have no prompt before them and Kimi's mapper
// gives them no turn (adapters/kimi/loop.go turnID, "" when no prompt
// opened one), where before the restart they belonged to their turn.
//
// Correct: the replay starts at a turn's prompt, or those records keep
// their turn. This test passes while the fault is present; when it fails
// the fault is fixed: delete it, and the edge allowance in restarted
// (drive_test.go).
func TestKnownFault_KimiReplayWindowLeavesItemsWithoutATurn(t *testing.T) {
	e := kimiEnv(t, 48*time.Hour)
	turns := make([]sessiongen.Turn, 230)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 20, Tools: 3, Output: 20}
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.Kimi{}, sessiongen.Script{Seed: 3, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if l.Records[l.Main()] <= 2000 {
		t.Fatalf("%d records: the window must be cut", l.Records[l.Main()])
	}
	st := support.SettleUntil(e.Rig(t, "kimi"), 230)
	orphans := support.Items(st, func(it harness.Item) bool { return it.TurnID == "" && (it.Tool != nil || it.Assistant != nil) })
	if len(orphans) == 0 {
		t.Fatal("every tool and reply has its turn: the fault is fixed (or the window fell on a turn's edge)")
	}
}

// Kimi killed at work leaves a tool with no result, a background job, an
// approval nobody answered. When the hub reports the agent exited they all
// end and nothing waits for the user; relaunched with --continue the same
// log carries on and the chat works.
func TestKimiExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/agent_exits", "kimi/lifecycle/relaunch", "kimi/capability/needs.approvals")
	extra := map[int]sessiongen.KimiTurn{
		3: {Approval: &sessiongen.KimiApproval{Command: "make", Open: true}},
	}
	l, r := kimiStart(t, false, extra, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Tools: 1, Say: 20}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := support.SettleUntil(r, 3)
	if st.Mission.Summary.NeedsYou != 1 || !st.Mission.Summary.Working {
		t.Fatalf("before the exit: %+v, want one thing waiting, working", st.Mission.Summary)
	}
	if q := openQuestions(st); len(q) != 1 {
		t.Fatalf("questions %v, want one open approval", q)
	}
	r.Hub("ended")
	l.Truth.Exited = true
	st = support.WaitFor(t, r, "the exit", func(st enginetest.State) bool {
		return !st.Mission.Summary.Working && st.Mission.Summary.NeedsYou == 0
	})
	for id, a := range openQuestions(st) {
		if a != nil {
			continue
		}
		t.Errorf("%s still open after the exit", id)
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
	// Relaunched with --continue: the same log, the hub says working.
	l.Truth.Exited = false
	r.Hub("working")
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20, Tools: 1, Output: 20}); err != nil {
		t.Fatal(err)
	}
	after := support.SettleUntil(r, 4)
	support.CheckEdge(t, "after the relaunch", after, l)
	r.Hub("idle")
	r.Restart()
	again := support.SettleUntil(r, 4)
	support.CheckEdge(t, "restart", again, l)
	support.Fail(t, "restart", support.OfferedAsks(after, support.ExitNotices(support.Restarted(after, again))))
}

// TestKnownFault_KimiKilledAgentsJobRunsAgainAfterARestart documents a
// fault: when the hub reports Kimi gone, its background job ends
// interrupted, as it should. Relaunched with --continue, Kimi carries on in
// the same log with no new header, so the log never says the job died
// (adapters/kimi/wire.go prompt() ends the turn but keeps w.tasks). A helper
// restart then reads the job as running again, for good.
//
// Correct: the job stays ended. This test passes while the fault is
// present; when it fails the fault is fixed: delete it.
func TestKnownFault_KimiKilledAgentsJobRunsAgainAfterARestart(t *testing.T) {
	l, r := kimiStart(t, false, map[int]sessiongen.KimiTurn{1: {Detached: 1}}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := support.SettleUntil(r, 1)
	if st.Mission.Summary.Background != 1 {
		t.Fatalf("%+v, want one job running", st.Mission.Summary)
	}
	r.Hub("ended")
	st = support.WaitFor(t, r, "the exit", func(st enginetest.State) bool { return st.Mission.Summary.Background == 0 })
	if bg := support.Items(st, support.OfKind(harness.KindBackground)); len(bg) != 1 || bg[0].Background.State != harness.StateInterrupted {
		t.Fatalf("job after the exit: %+v, want interrupted", bg)
	}
	r.Hub("idle") // relaunched; the log has no word of it
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	support.SettleUntil(r, 2)
	r.Restart()
	after := support.SettleUntil(r, 2)
	if after.Mission.Summary.Background != 1 {
		t.Fatal("the dead job stays ended after the restart: the fault is fixed")
	}
}

// A failed turn ends failed with the reason in words, and the next turn
// works; neither leaves the summary working; a restart reads the same.
func TestKimiFailedTurn(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/turn_end")
	l, r := kimiStart(t, false, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Error: true}, sessiongen.Turn{Say: 20})
	st := support.SettleUntil(r, 3)
	support.CheckEdge(t, "failed turn", st, l)
	if n := len(support.TurnsIn(st, harness.StateFailed)); n != 1 {
		t.Errorf("%d failed turns, want 1", n)
	}
	if n := len(support.TurnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns, want 2", n)
	}
	errs := support.Items(st, support.Notice(harness.NoticeError))
	if len(errs) != 1 || !strings.Contains(errs[0].Notice.Text, "sign-in") && !strings.Contains(errs[0].Notice.Text, "API key") {
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

// What Kimi's log shows of its capabilities: model, effort and mode,
// edited files, the todo list as the plan, a compaction, a background job
// and its end, approvals and questions with the answers given; paged
// history reads the same whatever the page size.
func TestKimiCapabilitiesInFiles(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/capability/chat", "kimi/capability/chat.history", "kimi/capability/model",
		"kimi/capability/mode", "kimi/capability/status", "kimi/capability/files", "kimi/capability/plan", "kimi/capability/compact",
		"kimi/capability/background", "kimi/capability/needs.approvals", "kimi/capability/needs.questions")
	extra := map[int]sessiongen.KimiTurn{
		1: {Model: "other/model", Effort: "low", Mode: "yolo", Edits: 2, Todos: []string{"done", "in_progress", "pending"}},
		2: {Approval: &sessiongen.KimiApproval{Command: "make", Decision: "approved"}, Plan: true},
		3: {Approval: &sessiongen.KimiApproval{Command: "make test", Decision: "approved", Scope: "session"}},
		4: {Approval: &sessiongen.KimiApproval{Command: "make clean", Decision: "rejected", Feedback: "Use make"}},
		5: {Question: &sessiongen.KimiQuestion{Question: "Which database?", Options: []string{"A", "B"}, Answer: "B"}},
		6: {Question: &sessiongen.KimiQuestion{Question: "Which features?", Options: []string{"Auth", "Search"}, Answer: "Auth, Search", Multi: true}},
	}
	turns := []sessiongen.Turn{{Say: 20, Jobs: 1}, {Say: 20}, {Say: 20}, {Say: 20}, {Say: 20}, {Say: 20, Compact: true}}
	l, r := kimiStart(t, false, extra, turns...)
	st := support.SettleUntil(r, 6)
	support.CheckEdge(t, "files", st, l)
	if got := support.StatusOf(st); got.Model != "other/model" || got.Effort != "low" || got.Mode != "plan" {
		t.Errorf("status model %q effort %q mode %q, want other/model low plan", got.Model, got.Effort, got.Mode)
	}
	got := map[string]string{}
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		q := it.Question
		if q.Answered == nil || len(q.Answered) == 0 {
			t.Errorf("question %s is open", it.ID)
			continue
		}
		got[q.Questions[0].Prompt] = strings.Join(q.Answered["0"], "+")
	}
	for prompt, want := range map[string]string{
		"Run this command?": "", // checked below by count
		"Which database?":   "B", "Which features?": "Auth+Search",
	} {
		if g, ok := got[prompt]; prompt != "Run this command?" && (!ok || g != want) {
			t.Errorf("%q answered %q (%v), want %q", prompt, g, ok, want)
		}
	}
	var decided []string
	for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
		if it.Question.Approve != "" {
			decided = append(decided, strings.Join(it.Question.Answered["0"], "+"))
		}
	}
	if strings.Join(decided, "|") != "Approve once|Approve for this session|Use make" {
		t.Errorf("approvals decided %v, want Approve once, Approve for this session, then the feedback", decided)
	}
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("needs you %d with every prompt answered", st.Mission.Summary.NeedsYou)
	}
	if p := st.Mission.Summary.Plan; p == nil || p.Done != 1 || p.Total != 3 {
		t.Errorf("plan summary %+v, want 1 of 3 done", p)
	}
	for _, sec := range st.Mission.Sections {
		if sec.Kind == harness.SectionPlan && len(sec.Rows) != 3 {
			t.Errorf("plan rows %d, want 3", len(sec.Rows))
		}
	}
	writes := support.Items(st, func(it harness.Item) bool { return it.Tool != nil && it.Tool.Name == "Write" })
	if len(writes) != 2 || len(writes[0].Tool.Files) != 1 {
		t.Errorf("writes %+v, want two with one file each", writes)
	}
	for _, it := range support.TurnsIn(st, harness.StateOK) {
		if it.Turn.FilesChanged != 2 && it.Turn.FilesChanged != 0 {
			t.Errorf("turn %s changed %d files, want 2 or 0", it.ID, it.Turn.FilesChanged)
		}
	}
	if c := support.Items(st, support.Notice(harness.NoticeCompacted)); len(c) != 1 {
		t.Errorf("%d compaction notices, want 1", len(c))
	}
	bg := support.Items(st, support.OfKind(harness.KindBackground))
	if len(bg) != 1 || bg[0].Background.State != harness.StateOK || bg[0].Background.Ended == nil {
		t.Errorf("background jobs %+v, want one that ended ok", bg)
	}
	for _, size := range []int{1, 4, 9} {
		support.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(size)))
		support.Fail(t, "paging", enginetest.Paging(r.ReadPages(size)))
	}
}

func TestKimiConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/connection_drop")
	support.DropAndReturn(t, kimiStarter)
}

func TestKimiSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/lifecycle/session_removed")
	support.RemoveAndServeAgain(t, kimiStarter)
}

func TestKimiConcurrentReaders(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/scale/concurrent_requests")
	support.ConcurrentReaders(t, kimiStarter)
}
