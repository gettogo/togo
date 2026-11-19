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

// aiderStart writes the turns as an Aider history of the last ten minutes
// and starts a rig over it.
func aiderStart(t *testing.T, extra map[int]sessiongen.AiderTurn, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := newEnv(t, 10*time.Minute)
	l, err := sessiongen.Write(e.home, &sessiongen.Aider{Extra: extra}, sessiongen.Script{Seed: 61, Start: e.start, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	return l, e.rig(t, "aider")
}

func aiderStarter(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	return aiderStart(t, nil, turns...)
}

// The history has no turn end: a prompt opens a turn, the next prompt
// closes it, and the last one stays open until the pane goes idle (the hub
// says so).
func TestAiderTurnEnd(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/turn_end")
	l, r := aiderStart(t, nil, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20, Tools: 1}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := settleUntil(r, 3)
	if n := len(turnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns while the last one is at work, want 2", n)
	}
	if n := len(turnsIn(st, harness.StateRunning)); n != 1 || !st.Mission.Summary.Working {
		t.Errorf("%d running turns, working %v: want the last turn running", n, st.Mission.Summary.Working)
	}
	r.Hub("idle")
	st = waitFor(t, r, "the idle close", func(st enginetest.State) bool { return len(turnsIn(st, harness.StateRunning)) == 0 })
	checkEdge(t, "idle", st, l)
	if n := len(turnsIn(st, harness.StateOK)); n != 3 {
		t.Errorf("%d finished turns after idle, want 3", n)
	}
	if st.Mission.Summary.Working {
		t.Error("working after the pane went idle")
	}
}

// A new run of aider in the folder (its /new, or a relaunch) writes a new
// header into the same file: the turn left open is closed interrupted, the
// old conversation stays, and the new run's prompts follow; a restart reads
// the same.
func TestAiderNewConversation(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/new_conversation", "aider/lifecycle/helper_restart")
	l, r := aiderStart(t, nil, sessiongen.Turn{Say: 20, Tools: 1}, sessiongen.Turn{Say: 20})
	settleUntil(r, 2)
	now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20}, sessiongen.Turn{Say: 20, Tools: 1}); err != nil {
		t.Fatal(err)
	}
	st := settleUntil(r, 4)
	r.Hub("idle")
	checkEdge(t, "after the new run", st, l)
	if n := len(userPrompts(st)); n != 4 {
		t.Errorf("%d prompts, want 4 (both runs)", n)
	}
	if n := len(turnsIn(st, harness.StateInterrupted)); n != 1 {
		t.Errorf("%d interrupted turns, want 1 (the turn open when the new run began)", n)
	}
	r.Restart()
	after := settleUntil(r, 4)
	checkEdge(t, "restart", after, l)
	fail(t, "restart", restarted(st, after))
}

// Aider killed at work leaves a prompt with no reply. When the hub reports
// the agent exited nothing is working or running any more; relaunched,
// aider writes a new header and the chat goes on. (How the killed turn
// ends is TestKnownFault_AiderKilledTurnEndsOK.)
func TestAiderExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/agent_exits", "aider/lifecycle/relaunch")
	l, r := aiderStart(t, map[int]sessiongen.AiderTurn{2: {Unfinished: true}}, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := settleUntil(r, 2)
	if !st.Mission.Summary.Working || len(turnsIn(st, harness.StateRunning)) != 1 {
		t.Fatalf("before the exit: working %v, %d running turns", st.Mission.Summary.Working, len(turnsIn(st, harness.StateRunning)))
	}
	r.Hub("ended")
	st = waitFor(t, r, "the exit", func(st enginetest.State) bool {
		return !st.Mission.Summary.Working && len(turnsIn(st, harness.StateRunning)) == 0
	})
	fail(t, "after the exit", enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"))
	r.Hub("working")
	now(l)
	if err := l.Append(sessiongen.Turn{New: true, Say: 20, Tools: 1}); err != nil {
		t.Fatal(err)
	}
	settleUntil(r, 3)
	r.Hub("idle")
	after := waitFor(t, r, "the relaunched turn's end", func(st enginetest.State) bool {
		return len(turnsIn(st, harness.StateRunning)) == 0 && len(userPrompts(st)) == 3
	})
	fail(t, "after the relaunch", enginetest.Without(enginetest.Check(after, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"))
	if after.Mission.Summary.Working {
		t.Error("working after the relaunched turn ended")
	}
}

// TestKnownFault_AiderKilledTurnEndsOK documents a fault: when the hub
// reports Aider exited at work, the engine first sees the working flag fall
// and has the mapper close the open turn as the pane going idle would
// (engine/sess.go statusChanged: setWorkingLocked before agentExitedLocked;
// adapters/aider/history.go CloseIdle, endState ok), so the turn of a
// prompt that got no reply ends ok, not interrupted.
//
// Correct: a turn open when the agent exits ends interrupted. This test
// passes while the fault is present; when it fails the fault is fixed:
// delete it.
func TestKnownFault_AiderKilledTurnEndsOK(t *testing.T) {
	_, r := aiderStart(t, map[int]sessiongen.AiderTurn{1: {Unfinished: true}}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	st := settleUntil(r, 1)
	if len(turnsIn(st, harness.StateRunning)) != 1 {
		t.Fatal("no turn at work before the exit")
	}
	r.Hub("ended")
	st = waitFor(t, r, "the exit", func(st enginetest.State) bool { return len(turnsIn(st, harness.StateRunning)) == 0 })
	if len(turnsIn(st, harness.StateInterrupted)) != 0 || len(turnsIn(st, harness.StateOK)) != 1 {
		t.Fatalf("the killed turn: %d ok, %d interrupted: the fault is fixed", len(turnsIn(st, harness.StateOK)), len(turnsIn(st, harness.StateInterrupted)))
	}
}

// TestKnownFault_AiderTurnEndIsNotInTheFile documents a fault: Aider's
// history has no turn end, the pane going idle closes the turn only while
// the helper watches it happen (engine/sess.go setWorkingLocked closes on a
// working-to-idle change, not while restoring), and a restart reads the
// file again. After it the last turn runs for good (until the next
// prompt), and a turn that a later header ended ok before now ends
// interrupted.
//
// Correct: a restart while the pane is idle shows the turns ended. This
// test passes while the fault is present; when it fails the fault is fixed:
// delete it, and see aider/history.go CloseIdle.
func TestKnownFault_AiderTurnEndIsNotInTheFile(t *testing.T) {
	l, r := aiderStart(t, nil, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	settleUntil(r, 2)
	r.Hub("idle")
	live := waitFor(t, r, "the idle close", func(st enginetest.State) bool { return len(turnsIn(st, harness.StateRunning)) == 0 })
	now(l)
	r.Restart()
	after := settleUntil(r, 2)
	if len(turnsIn(after, harness.StateRunning)) != 1 || len(turnsIn(live, harness.StateOK)) != 2 {
		t.Fatalf("after a restart: %d running turns (live: %d ok): the fault is fixed", len(turnsIn(after, harness.StateRunning)), len(turnsIn(live, harness.StateOK)))
	}
}

// A request the server refuses ends the turn failed with the reason in
// words; the next turn works.
func TestAiderFailedTurn(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/turn_end")
	l, r := aiderStart(t, nil, sessiongen.Turn{Say: 20}, sessiongen.Turn{Error: true}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	settleUntil(r, 3)
	r.Hub("idle")
	st := waitFor(t, r, "the turns' end", func(st enginetest.State) bool { return len(turnsIn(st, harness.StateRunning)) == 0 })
	checkEdge(t, "failed turn", st, l)
	if n := len(turnsIn(st, harness.StateFailed)); n != 1 {
		t.Errorf("%d failed turns, want 1", n)
	}
	if n := len(turnsIn(st, harness.StateOK)); n != 2 {
		t.Errorf("%d finished turns, want 2", n)
	}
	errs := items(st, notice(harness.NoticeError))
	if len(errs) != 1 || !strings.Contains(errs[0].Notice.Text, "no router for requested model") {
		t.Errorf("error notices %+v: want one naming the model the server does not route", errs)
	}
}

// What Aider's history shows of its capabilities: the files added to the
// chat, edits with their files, commits, shell runs, the reply as
// markdown; paged history reads the same whatever the page size.
func TestAiderCapabilitiesInFiles(t *testing.T) {
	coverage.Covers(t, "generated", "aider/capability/chat", "aider/capability/files", "aider/capability/changes")
	extra := map[int]sessiongen.AiderTurn{1: {Files: 2, Edits: 2, Commit: true}, 2: {Edits: 1}}
	l, r := aiderStart(t, extra, sessiongen.Turn{Say: 200, Tools: 2, Output: 5}, sessiongen.Turn{Say: 20}, sessiongen.Turn{Say: 20})
	r.Hub("working")
	settleUntil(r, 3)
	r.Hub("idle")
	st := waitFor(t, r, "the end", func(st enginetest.State) bool { return len(turnsIn(st, harness.StateRunning)) == 0 })
	checkEdge(t, "files", st, l)
	byName := map[string][]harness.Item{}
	for _, it := range items(st, ofKind(harness.KindTool)) {
		byName[it.Tool.Name] = append(byName[it.Tool.Name], it)
	}
	if edits := byName["edit"]; len(edits) != 3 {
		t.Errorf("%d edit steps, want 3", len(edits))
	} else {
		for _, e := range edits {
			if len(e.Tool.Files) != 1 || !strings.HasSuffix(e.Tool.Files[0].Path, ".py") || e.Tool.Category != harness.ToolEdit {
				t.Errorf("edit %+v: want one changed .py file", e.Tool)
			}
		}
	}
	if c := byName["commit"]; len(c) != 1 || !strings.HasPrefix(c[0].Tool.Title, "Commit ") {
		t.Errorf("commits %+v, want one", c)
	}
	var added bool
	for _, a := range byName["aider"] {
		added = added || strings.HasPrefix(a.Tool.Title, "Added f1_0.py to the chat") && strings.Contains(a.Tool.Output, "f1_1.py")
	}
	if !added {
		t.Errorf("no step for the two files added to the chat: %+v", byName["aider"])
	}
	runs := byName["run"]
	if len(runs) != 2 || runs[0].Tool.Category != harness.ToolShell {
		t.Errorf("shell runs %+v, want two shell steps", runs)
	}
	turnFiles := 0
	for _, it := range items(st, func(it harness.Item) bool { return it.Turn != nil }) {
		turnFiles += it.Turn.Steps
	}
	if turnFiles < 8 {
		t.Errorf("%d steps counted over the turns, want at least 8", turnFiles)
	}
	if n := len(items(st, ofKind(harness.KindAssistant))); n < 3 {
		t.Errorf("%d replies, want at least 3", n)
	}
	for _, size := range []int{1, 4, 9} {
		fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(size)))
		fail(t, "paging", enginetest.Paging(r.ReadPages(size)))
	}
}

func TestAiderConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/connection_drop")
	dropAndReturn(t, aiderStarter)
}

func TestAiderSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "aider/lifecycle/session_removed")
	removeAndServeAgain(t, aiderStarter)
}

func TestAiderConcurrentReaders(t *testing.T) {
	coverage.Covers(t, "generated", "aider/scale/concurrent_requests")
	concurrentReaders(t, aiderStarter)
}

// TestKnownFault_AiderHistoryOver8MiBShowsNothingAfterARestart documents a
// fault: aider appends every run in the folder to one history file, so it
// grows without end. A restart replays only the last 8 MiB of a file
// (source/file.go MaxFileRead), and the mapper maps nothing until it sees a
// run's "# aider chat started" header (adapters/aider/history.go, `active`):
// when the replay begins after the last header, the chat stays empty, and
// stays so until aider next starts.
//
// Correct: the newest conversation shows. This test passes while the fault
// is present; when it fails the fault is fixed: delete it.
func TestKnownFault_AiderHistoryOver8MiBShowsNothingAfterARestart(t *testing.T) {
	e := newEnv(t, 48*time.Hour)
	turns := make([]sessiongen.Turn, 700)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 12_000}
	}
	l, err := sessiongen.Write(e.home, &sessiongen.Aider{}, sessiongen.Script{Seed: 9, Start: e.start, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(l.Main()); err != nil || fi.Size() <= 8<<20 {
		t.Fatalf("history is %d bytes (%v): want above 8 MiB", fi.Size(), err)
	}
	r := e.rig(t, "aider")
	if !never(r, 4*time.Second, func(st enginetest.State) bool { return len(st.Items) > 0 }) {
		t.Fatal("the history shows: the fault is fixed")
	}
}
