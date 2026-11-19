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

// piStart writes the turns as a Pi session of the last ten minutes and
// starts a rig with a process view over it.
func piStart(t *testing.T, extra map[int]sessiongen.PiTurn, turns ...sessiongen.Turn) (support.Env, *sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := support.NewEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.Home, &sessiongen.Pi{Extra: extra}, sessiongen.Script{Seed: 31, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	return e, l, e.RigMod(t, "pi", func(o *enginetest.Options) { o.PanePIDs = support.ProcsOf(t) })
}

func piStarter(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	_, l, r := piStart(t, nil, turns...)
	return l, r
}

// /new in Pi starts a new file in the folder. The session moves to it,
// starts the chat over on it, follows the new file after a helper
// restart, and holds no item twice.
func TestPiNewConversation(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/new_conversation", "pi/lifecycle/helper_restart")
	_, l, r := piStart(t, nil, sessiongen.Turn{Say: 20, Tools: 1, Output: 30}, sessiongen.Turn{Say: 20})
	support.CheckEdge(t, "first conversation", support.SettleUntil(r, 2), l)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20, Tools: 1, Output: 30}, sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d files", len(l.Mains))
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
	// The restart follows the new conversation, not the first file.
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	more := support.SettleUntil(r, 3)
	support.CheckEdge(t, "new conversation after restart", more, l)
	// Stable ids: every id once.
	support.Fail(t, "paging", enginetest.Paging(more))
}

// Pi resumed on an older conversation it had not followed appends to that
// file; the session moves to it.
func TestPiResumeOlderConversation(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/resume")
	e := support.NewEnv(t, 20*time.Minute)
	// Two conversations exist when the session starts; the newer is followed.
	l, err := sessiongen.Write(e.Home, &sessiongen.Pi{Extra: map[int]sessiongen.PiTurn{3: {Resume: 1}}}, sessiongen.Script{Seed: 33, Start: e.Start, Cwd: e.Cwd,
		Turns: []sessiongen.Turn{{Say: 20}, {Say: 20, Tools: 1, Output: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append(sessiongen.Turn{New: true, Say: 20}); err != nil {
		t.Fatal(err)
	}
	r := e.RigMod(t, "pi", func(o *enginetest.Options) { o.PanePIDs = support.ProcsOf(t) })
	support.CheckEdge(t, "start", support.SettleUntil(r, 3), l)
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20}); err != nil { // the resume: prompt 4, first file
		t.Fatal(err)
	}
	st := support.SettleUntil(r, 4)
	support.CheckEdge(t, "after resume", st, l)
	support.Fail(t, "paging", enginetest.Paging(st))
}

// Pi appends and closes its file per entry. After a /new, a resume of
// the first conversation appends to the file the session left: the
// session follows it back (resumeToLeft).
func TestPiResumeToLeftConversation(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/resume")
	_, l, r := piStart(t, map[int]sessiongen.PiTurn{3: {Resume: 1}}, sessiongen.Turn{Say: 20})
	resumeToLeft(t, l, r)
}

// Pi killed mid-turn leaves a prompt and a tool call with no end. When
// the hub reports the agent exited, the turn and tool end interrupted and
// the summary stops working; relaunched on the same file it carries on.
func TestPiExitAndRelaunch(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/agent_exits", "pi/lifecycle/relaunch")
	_, l, r := piStart(t, map[int]sessiongen.PiTurn{2: {Unfinished: true}}, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Tools: 1, Output: 20})
	r.Hub("working")
	st := support.SettleUntil(r, 2)
	if !st.Mission.Summary.Working || len(support.TurnsIn(st, harness.StateRunning)) != 1 {
		t.Fatalf("before the exit: working %v, %d running turns, want a running turn", st.Mission.Summary.Working, len(support.TurnsIn(st, harness.StateRunning)))
	}
	r.Hub("ended")
	st = support.WaitFor(t, r, "the exit", func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	if n := len(support.TurnsIn(st, harness.StateRunning)); n != 0 {
		t.Errorf("%d turns still running after the exit", n)
	}
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		if it.Tool.State == harness.StateRunning {
			t.Errorf("tool %s still running after the exit", it.ID)
		}
	}
	if n := len(support.TurnsIn(st, harness.StateInterrupted)); n != 1 {
		t.Errorf("%d interrupted turns, want 1", n)
	}
	// The exit notice has no turn, so the interrupted turn's prompt has no
	// reply of its own: that is not a lost reply.
	support.Fail(t, "after the exit", enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"))
	// Relaunched with --session: the same file grows, the hub says working.
	r.Hub("working")
	support.Now(l)
	if err := l.Append(sessiongen.Turn{Say: 20, Tools: 1, Output: 20}); err != nil {
		t.Fatal(err)
	}
	st = support.SettleUntil(r, 3)
	support.CheckEdge(t, "after the relaunch", st, l)
	if n := len(support.TurnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns after the relaunch, want 2", n)
	}
	r.Hub("idle")
	// A restart over the finished file agrees with the live view.
	r.Restart()
	after := support.SettleUntil(r, 3)
	support.CheckEdge(t, "restart", after, l)
	support.Fail(t, "restart", support.ExitNotices(support.Restarted(st, after)))
}

// An interrupted turn ends interrupted with a notice; a failed request
// ends the turn failed with the reason in words. Neither leaves the
// summary working, and a restart reads the same.
func TestPiInterruptAndError(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/interrupt", "pi/lifecycle/turn_end")
	_, l, r := piStart(t, nil, sessiongen.Turn{Say: 20}, sessiongen.Turn{Interrupt: true}, sessiongen.Turn{Error: true}, sessiongen.Turn{Say: 20})
	st := support.SettleUntil(r, 4)
	support.CheckEdge(t, "interrupt and error", st, l)
	if n := len(support.TurnsIn(st, harness.StateInterrupted)); n != 1 {
		t.Errorf("%d interrupted turns, want 1", n)
	}
	if n := len(support.TurnsIn(st, harness.StateFailed)); n != 1 {
		t.Errorf("%d failed turns, want 1", n)
	}
	if n := len(support.TurnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns, want 2", n)
	}
	if n := len(support.Items(st, support.Notice(harness.NoticeInterrupted))); n != 1 {
		t.Errorf("%d interrupted notices, want 1", n)
	}
	errs := support.Items(st, support.Notice(harness.NoticeError))
	if len(errs) != 1 || !strings.Contains(errs[0].Notice.Text, "sign-in") && !strings.Contains(errs[0].Notice.Text, "API key") {
		t.Errorf("error notices %+v: want one that says the key was refused", errs)
	}
	if st.Mission.Summary.Working {
		t.Error("working after the last turn ended")
	}
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		if it.Tool.State == harness.StateRunning {
			t.Errorf("tool %s running after its turn ended", it.ID)
		}
	}
	r.Restart()
	after := support.SettleUntil(r, 4)
	support.CheckEdge(t, "restart", after, l)
	support.Fail(t, "restart", support.Restarted(st, after))
}

// What Pi's files show of its capabilities: model and thinking-level
// changes, edited files, a compaction, a command run with "!", thinking
// blocks, the title; paged history reads the same whatever the page size.
func TestPiCapabilitiesInFiles(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/capability/chat", "pi/capability/chat.history", "pi/capability/model", "pi/capability/effort",
		"pi/capability/status", "pi/capability/changes", "pi/capability/files", "pi/capability/compact")
	extra := map[int]sessiongen.PiTurn{
		2: {Model: "openai/gpt-5", Effort: "high", Edits: 2, Think: true},
		3: {Bash: "git status", Edits: 1},
		4: {Model: "moonshotai/kimi-k2.6", Title: "Parser fix"},
	}
	turns := []sessiongen.Turn{{Say: 20}, {Say: 20, Tools: 1, Output: 20}, {Say: 20}, {Say: 20, Compact: true}}
	_, l, r := piStart(t, extra, turns...)
	st := support.SettleUntil(r, 4)
	support.CheckEdge(t, "files", st, l)
	if got := support.StatusOf(st); got.Model != "moonshotai/kimi-k2.6" || got.Effort != "high" {
		t.Errorf("status model %q effort %q, want moonshotai/kimi-k2.6 high", got.Model, got.Effort)
	}
	models := support.Items(st, support.Notice(harness.NoticeModel))
	if len(models) != 2 || models[0].Notice.Text != "Model: openai/gpt-5" || models[1].Notice.Text != "Model: moonshotai/kimi-k2.6" {
		t.Errorf("model notices %+v, want gpt-5 then kimi-k2.6", models)
	}
	if c := support.Items(st, support.Notice(harness.NoticeCompacted)); len(c) != 1 || !strings.Contains(c[0].Notice.Text, "120000") {
		t.Errorf("compaction notices %+v, want one naming 120000 tokens", c)
	}
	edits := support.Items(st, func(it harness.Item) bool { return it.Tool != nil && it.Tool.Category == harness.ToolEdit })
	if len(edits) != 3 {
		t.Fatalf("%d edit tools, want 3", len(edits))
	}
	for _, it := range edits {
		if len(it.Tool.Files) != 1 || it.Tool.State != harness.StateOK || !strings.HasPrefix(it.Tool.Files[0].Path, "e") {
			t.Errorf("edit %s: %+v, want one changed file (relative to the folder)", it.ID, it.Tool)
		}
	}
	changed := map[int]int{}
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Turn != nil }) {
		changed[it.Turn.FilesChanged]++
	}
	if changed[2] != 1 || changed[1] != 1 || changed[0] != 2 {
		t.Errorf("files changed per turn %v, want one turn with 2, one with 1, two with 0", changed)
	}
	if b := support.Items(st, func(it harness.Item) bool {
		return it.Tool != nil && it.Tool.Category == harness.ToolShell && it.Tool.Title == "git status"
	}); len(b) != 1 || b[0].Tool.ExitCode == nil || *b[0].Tool.ExitCode != 0 {
		t.Errorf("command run with !: %+v", b)
	}
	if n := len(support.Items(st, support.OfKind(harness.KindThinking))); n != 1 {
		t.Errorf("%d thinking items, want 1", n)
	}
	for _, size := range []int{1, 3, 7} {
		support.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(size)))
		support.Fail(t, "paging", enginetest.Paging(r.ReadPages(size)))
	}
}

// A phone that drops while Pi works and returns with the cursor it had
// gets exactly the items it missed.
func TestPiConnectionDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/connection_drop")
	support.DropAndReturn(t, piStarter)
}

// A removed session is dropped by the engine; serving it again rebuilds
// the conversation from the files.
func TestPiSessionRemoved(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/lifecycle/session_removed")
	support.RemoveAndServeAgain(t, piStarter)
}

// TestKnownFault_PiReplayWindowStartsATurnMidTurn documents a fault: a
// restart replays the last 2000 records of a long file. When the window
// begins inside a turn, the first record Pi's mapper sees is an assistant
// message with no prompt before it, and the mapper opens a turn for it
// (adapters/pisession/message.go:92-94, startTurn on an assistant message
// when no turn is open). The restored chat then holds a turn with no
// prompt, and the items of that turn change turn: before the restart they
// belonged to the real turn.
//
// Correct: records of a turn whose prompt is outside the window keep the
// turn they had, or belong to none, as omp's mapper does
// (adapters/omp/session.go turnID). This test passes while the fault is
// present; when it fails the fault is fixed: delete it, and the edge-turn
// allowance in restarted (drive_test.go).
func TestKnownFault_PiReplayWindowStartsATurnMidTurn(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, 48*time.Hour)
	turns := make([]sessiongen.Turn, 230)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 20, Tools: 4, Output: 20}
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.Pi{}, sessiongen.Script{Seed: 3, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if l.Records[l.Main()] <= 2000 {
		t.Fatalf("%d records: the window must be cut", l.Records[l.Main()])
	}
	r := e.Rig(t, "pi")
	st := support.SettleUntil(r, 230)
	withPrompt := map[string]bool{}
	for _, it := range support.UserPrompts(st) {
		withPrompt[it.TurnID] = true
	}
	orphans := 0
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Turn != nil }) {
		if !withPrompt[it.ID] {
			orphans++
			t.Logf("turn %s has no prompt in the chat", it.ID)
		}
	}
	if orphans == 0 {
		t.Fatal("every turn has its prompt: the fault is fixed (or the window fell on a turn's edge)")
	}
}

// Many clients reading at once while Pi writes get consistent answers.
func TestPiConcurrentReaders(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "pi/scale/concurrent_requests")
	support.ConcurrentReaders(t, piStarter)
}
