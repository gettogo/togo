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

var vibeAgent = fileAgent{name: "vibe", profile: func() sessiongen.Profile { return &sessiongen.Vibe{} }, cont: sessiongen.Continue}

// A restart that falls between two turns shows nothing: see
// TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing. Every scenario below
// restarts inside a turn.

// A session of more than 1,000 history entries, restored mid-turn: only the
// newest 1,000 entries come back (vibe/history.go maxHistoryEntries), the
// open turn is whole, and two restores agree.
func TestVibeLongSession(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "vibe/scale/long_session", "vibe/capability/chat", "vibe/capability/chat.history", "vibe/scale/at_limit")
	n := 330
	if support.LongMode() {
		n = 1500
	}
	e := support.NewEnv(t, 48*time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 11})
	r.Stop()
	turns := make([]sessiongen.Turn, 0, n+1)
	for i := 0; i < n; i++ {
		turns = append(turns, sessiongen.Turn{Say: 300, Tools: 2, Output: 1500})
	}
	vibeAgent.add(t, l, append(turns, open(sessiongen.Turn{Say: 50, Tools: 2, Output: 200}))...)
	r.Start()
	st := vibeAgent.seen(t, r, "restored", l, true)
	entries := 0
	for _, it := range st.Items {
		if it.Kind != harness.KindTurn {
			entries++
		}
	}
	if entries > 1000+8 || entries < 900 {
		t.Errorf("%d chat items after the restore, want about the newest 1,000 entries", entries)
	}
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
	r.Restart()
	second := vibeAgent.seen(t, r, "restored again", l, true)
	support.Fail(t, "second restore", enginetest.Reread(st, second), l)
	// Past the limit the open turn is still followed to its end.
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40})
	vibeAgent.seen(t, r, "turn ended", l, false)
}

// One turn of more than 2,000 journal records (1,100 tool calls): the replay
// of a restore keeps the newest 2,000 records and the segment's first
// (source/file.go MaxRecords), so the turn's prompt and early calls are lost
// but the rest, and the end of the turn, are followed.
func TestVibeTurnPastTheRecordLimit(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "vibe/scale/at_limit")
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 12})
	r.Stop()
	vibeAgent.add(t, l, sessiongen.Turn{Say: 50, Tools: 3, Output: 100}, open(sessiongen.Turn{Say: 50, Tools: 1100, Output: 100}))
	if got := l.Records[l.Main()]; got != 0 {
		t.Logf("main file %d", got)
	}
	r.Start()
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return enginetest.Has(l.Truth.Last)(st) && len(st.Items) > 100 })
	tools := 0
	for _, it := range st.Items {
		if it.Tool != nil {
			tools++
		}
	}
	if tools < 600 {
		t.Errorf("%d tool rows after the restore of a 1,100 call turn", tools)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40})
	vibeAgent.seen(t, r, "turn ended", l, false)
}

func TestVibeRestartsAtSeededPoints(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/helper_restart")
	if n := vibeAgent.restartsAtSeededPoints(t, 71, 14, 4); n != 4 {
		t.Fatalf("%d restarts", n)
	}
}

func TestVibeConnectionDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/connection_drop")
	vibeAgent.reconnect(t)
}

func TestVibeSessionRemoved(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/session_removed")
	vibeAgent.removed(t)
}

func TestVibeConcurrentRequests(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/scale/concurrent_requests")
	vibeAgent.hammer(t)
}

func itemsOfKind(st enginetest.State, kind string) []harness.Item {
	var out []harness.Item
	for _, it := range latestItems(st) {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func the(t *testing.T, st enginetest.State, kind string) harness.Item {
	t.Helper()
	xs := itemsOfKind(st, kind)
	if len(xs) != 1 {
		t.Fatalf("%d %s items, want 1", len(xs), kind)
	}
	return xs[0]
}

// A tool asks for permission, waits, and is allowed; the next is denied.
func TestVibeApprovals(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/capability/needs.approvals")
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 13})
	vibeAgent.add(t, l, open(with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve = 1 })))
	st := vibeAgent.seen(t, r, "waiting", l, true)
	q := the(t, st, harness.KindQuestion)
	if q.Question.Answered != nil || q.Question.Approve != "Allow once" || q.Question.Deny != "Deny" || len(q.Question.Questions[0].Options) != 4 {
		t.Fatalf("question %+v", q.Question)
	}
	if !strings.HasPrefix(q.Question.Questions[0].Prompt, "Permission for the file_system.bash tool (echo *)") {
		t.Errorf("prompt %q", q.Question.Questions[0].Prompt)
	}
	if tool := the(t, st, harness.KindTool); tool.Tool.State != harness.StateWaiting {
		t.Errorf("the gated tool is %s, want waiting", tool.Tool.State)
	}
	if st.Mission.Summary.NeedsYou != 1 {
		t.Errorf("summary %+v, want one thing needing the user", st.Mission.Summary)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 30})
	st = vibeAgent.seen(t, r, "allowed", l, false)
	if q := the(t, st, harness.KindQuestion); q.Question.Answered["0"][0] != "Allow once" {
		t.Errorf("answered %v", q.Question.Answered)
	}
	if tool := the(t, st, harness.KindTool); tool.Tool.State != harness.StateOK || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("tool %s, summary %+v", tool.Tool.State, st.Mission.Summary)
	}
	// Denied, in a new turn.
	vibeAgent.add(t, l, with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve, h.Fail = 1, 1 }))
	st = vibeAgent.seen(t, r, "denied", l, false)
	denied := 0
	for _, it := range itemsOfKind(st, harness.KindTool) {
		if it.Tool.State == harness.StateDenied {
			denied++
		}
	}
	if denied != 1 {
		t.Errorf("%d denied tools, want 1", denied)
	}
	var answers []string
	for _, q := range itemsOfKind(st, harness.KindQuestion) {
		answers = append(answers, q.Question.Answered["0"]...)
	}
	if len(answers) != 2 {
		t.Errorf("answers %v, want Allow once and Deny", answers)
	}
	// An approval still open when the turn is interrupted closes unanswered.
	vibeAgent.add(t, l, open(with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve = 1 })))
	vibeAgent.seen(t, r, "waiting again", l, true)
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: "next"})
	st = vibeAgent.seen(t, r, "next turn", l, false)
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("an unanswered approval stays open after the next turn: %+v", st.Mission.Summary)
	}
}

func TestVibeQuestions(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/capability/needs.questions")
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 14})
	ask := func(a *sessiongen.HookAsk, pending bool) sessiongen.Turn {
		return with(sessiongen.Turn{Say: 30}, func(h *sessiongen.HookTurn) { h.Ask, h.Pending = a, pending })
	}
	a := &sessiongen.HookAsk{Prompt: "Which database?", Options: []string{"Postgres", "SQLite"}, Answer: "SQLite"}
	vibeAgent.add(t, l, ask(a, true))
	st := vibeAgent.seen(t, r, "open", l, true)
	q := the(t, st, harness.KindQuestion)
	if q.Question.Answered != nil || q.Question.Questions[0].Prompt != "Which database?" || len(q.Question.Questions[0].Options) != 2 || st.Mission.Summary.NeedsYou != 1 {
		t.Fatalf("open question %+v, summary %+v", q.Question, st.Mission.Summary)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 30})
	st = vibeAgent.seen(t, r, "answered", l, false)
	if got := the(t, st, harness.KindQuestion).Question.Answered["0"]; len(got) != 1 || got[0] != "SQLite" {
		t.Errorf("answered %v", got)
	}
	vibeAgent.add(t, l, ask(&sessiongen.HookAsk{Prompt: "Skip it?", Options: []string{"Yes"}, Cancel: true}, false))
	st = vibeAgent.seen(t, r, "cancelled", l, false)
	for _, q := range itemsOfKind(st, harness.KindQuestion) {
		if q.Question.Questions[0].Prompt == "Skip it?" && (q.Question.Answered == nil || len(q.Question.Answered["0"]) != 0) {
			t.Errorf("a cancelled question is %v, want closed with no answer", q.Question.Answered)
		}
	}
	// A question left open when the next prompt comes is closed.
	vibeAgent.add(t, l, ask(&sessiongen.HookAsk{Prompt: "Left open?", Options: []string{"A"}}, true))
	vibeAgent.seen(t, r, "open again", l, true)
	vibeAgent.add(t, l, sessiongen.Turn{Prompt: "moving on", Say: 20})
	st = vibeAgent.seen(t, r, "moved on", l, false)
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("summary %+v after the question was left", st.Mission.Summary)
	}
}

func notices(st enginetest.State) []harness.Item { return itemsOfKind(st, harness.KindNotice) }

// A turn the user interrupts ends interrupted, with no notice, and the
// next turn runs on; one that fails says why.
func TestVibeInterruptAndFailure(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/interrupt", "vibe/lifecycle/turn_end", "vibe/capability/interrupt")
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 15})
	vibeAgent.attach(t, r, l)
	st := vibeAgent.seen(t, r, "done", l, false)
	if the(t, st, harness.KindTurn).Turn.State != harness.StateOK || st.Mission.Summary.Working {
		t.Fatalf("turn %+v summary %+v", the(t, st, harness.KindTurn).Turn, st.Mission.Summary)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Tools: 1, Output: 50, Interrupt: true})
	st = vibeAgent.seen(t, r, "interrupted", l, false)
	interrupted := 0
	for _, it := range itemsOfKind(st, harness.KindTurn) {
		if it.Turn.State == harness.StateInterrupted {
			interrupted++
		}
	}
	if interrupted != 1 || len(notices(st)) != 0 || st.Mission.Summary.Working {
		t.Errorf("%d interrupted turns, %d notices, summary %+v", interrupted, len(notices(st)), st.Mission.Summary)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Tools: 1, Error: true})
	st = vibeAgent.seen(t, r, "failed", l, false)
	var n *harness.NoticeBody
	for _, it := range notices(st) {
		n = it.Notice
	}
	if n == nil || n.Reason != harness.ErrAuth || n.Level != "error" {
		t.Errorf("notice %+v, want an authentication failure", n)
	}
	vibeAgent.add(t, l, sessiongen.Turn{Say: 30})
	st = vibeAgent.seen(t, r, "after the failure", l, false)
	if st.Mission.Summary.Working || len(itemsOfKind(st, harness.KindTurn)) != 4 {
		t.Errorf("%d turns, summary %+v", len(itemsOfKind(st, harness.KindTurn)), st.Mission.Summary)
	}
}

// The agent exits in the middle of a turn: nothing is left running, and the
// chat says so. Started again (--continue), the session goes on in its
// next segment.
func TestVibeExitAndRelaunch(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/agent_exits", "vibe/lifecycle/relaunch", "vibe/lifecycle/resume", "vibe/capability/resume")
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 16})
	vibeAgent.attach(t, r, l)
	vibeAgent.add(t, l, open(sessiongen.Turn{Say: 30, Tools: 2, Output: 50}))
	r.SetStatus("working")
	vibeAgent.seen(t, r, "mid turn", l, true)
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	if !st.Mission.Summary.Working {
		t.Fatalf("summary %+v, want working", st.Mission.Summary)
	}
	r.SetEnded(true)
	gone := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	for _, it := range gone.Items {
		if it.Tool != nil && support.Running(it.Tool.State) {
			t.Errorf("tool %s is %s after the agent exited", it.ID, it.Tool.State)
		}
	}
	turnOf := func(st enginetest.State, state string) int {
		n := 0
		for _, it := range itemsOfKind(st, harness.KindTurn) {
			if it.Turn.State == state {
				n++
			}
		}
		return n
	}
	if turnOf(gone, harness.StateInterrupted) != 1 {
		t.Errorf("the turn the agent died in is not interrupted")
	}
	r.SetEnded(false)
	l.Truth.Exited = false
	vibeAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	st = r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	if st.Mission.Summary.Working || len(itemsOfKind(st, harness.KindTurn)) != 3 {
		t.Errorf("%d turns, summary %+v after the relaunched turn", len(itemsOfKind(st, harness.KindTurn)), st.Mission.Summary)
	}
	support.Fail(t, "relaunched", enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), "prompt-unanswered"), l)
	// TestKnownFault_Vibe_ExitedTurnBecomesOKAtRelaunch: the turn the agent
	// died in is now ok, and is excluded from the rule above.
	if turnOf(st, harness.StateInterrupted) != 0 && turnOf(st, harness.StateOK) != 3 {
		t.Logf("the interrupted turn stayed interrupted")
	}
}

// TestKnownFault_Vibe_ExitedTurnBecomesOKAtRelaunch documents a fault: the
// turn Vibe died in is shown interrupted, and when the relaunched Vibe starts
// its next turn the first one is shown ok, as if it had finished. Correct: it
// stays interrupted. The cause is vibe/session.go:221-224, where a new turn
// closes earlier ones the mapper still holds open with state ok; the exit was
// only drawn by the engine. This passes while the fault is present.
func TestKnownFault_Vibe_ExitedTurnBecomesOKAtRelaunch(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 19})
	vibeAgent.add(t, l, open(sessiongen.Turn{Say: 30, Tools: 1, Output: 50}))
	vibeAgent.seen(t, r, "mid turn", l, true)
	r.SetEnded(true)
	r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		for _, it := range itemsOfKind(st, harness.KindTurn) {
			return it.Turn.State == harness.StateInterrupted
		}
		return false
	})
	r.SetEnded(false)
	vibeAgent.add(t, l, sessiongen.Turn{Say: 30})
	st := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	for _, it := range itemsOfKind(st, harness.KindTurn) {
		if it.Turn.State == harness.StateInterrupted {
			t.Fatal("the turn the agent died in stays interrupted: the fault is fixed")
		}
	}
}

// Vibe started again without --continue writes a new session folder, and
// vibe --resume a new segment of an earlier one: the session follows each
// (vibe/vibe.go journalFor), keeping the earlier conversation's items.
// Keeping both conversations is this agent's current convention; the
// question whether every agent should start over instead is open.
func TestVibeFollowsNewConversationAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/lifecycle/new_conversation")
	followsConversations(t, vibeAgent, 17)
}

// TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing documents a fault: a
// helper that starts while Vibe is between two turns shows an empty chat.
// Correct: the chat holds the session's history (the snapshot), as it does
// when the restart falls inside a turn. After each turn Vibe writes a
// snapshot and an empty next journal segment (adapters/vibe/testdata/home,
// manifest.json recovery_journal_segment); the segment feed follows only the
// newest file of the glob when it starts (source/file.go:262, the `i == 0`
// branch of watchGlob), and the mapper reads the snapshot only with its
// first journal record (vibe/session.go:164-169), which never comes. The
// next prompt brings the whole history back. This passes while the fault is
// present.
func TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing(t *testing.T) {
	t.Parallel()
	e := support.NewEnv(t, time.Hour)
	l, r := vibeAgent.start(t, e, sessiongen.Script{Seed: 18})
	r.Stop()
	vibeAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 2, Output: 50}, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	r.Start()
	time.Sleep(4 * time.Second)
	if n := len(r.Read().Items); n != 0 {
		t.Fatalf("%d items shown: the fault is fixed; restart between turns in the scenarios", n)
	}
	vibeAgent.add(t, l, open(sessiongen.Turn{Say: 30, Tools: 1, Output: 50}))
	st := vibeAgent.seen(t, r, "next prompt", l, true)
	if len(st.Items) < 10 {
		t.Errorf("%d items after the next prompt, want the whole history", len(st.Items))
	}
}
