package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

var crushAgent = fileAgent{name: "crush", profile: func() sessiongen.Profile { return &sessiongen.Crush{} }, cont: sessiongen.Continue}

// A session past the feed's page of 500 rows is read in two pages, ten
// seconds apart (TestKnownFault_Crush_PagesOfALongSessionComeTenSecondsApart),
// so this one is 520 rows; TOGO_E2E_LONG=1 makes it 3,200. The helper
// restores it, restarts, and shows the same chat.
func TestCrushLongSession(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/scale/long_session", "crush/capability/chat", "crush/capability/chat.history", "crush/scale/at_limit")
	n := 130
	if longRun() {
		n = 800
	}
	ca := crushAgent
	ca.limit = time.Duration(10*(n/125)+30) * time.Second
	e := newEnv(t, 48*time.Hour)
	l, r := ca.start(t, e, sessiongen.Script{Seed: 31})
	r.Stop()
	turns := make([]sessiongen.Turn, 0, n+1)
	for i := 0; i < n; i++ {
		turns = append(turns, sessiongen.Turn{Say: 300, Tools: 1, Output: 1500})
	}
	ca.add(t, l, append(turns, open(sessiongen.Turn{Say: 50, Tools: 1, Output: 200}))...)
	r.Start()
	st := ca.seen(t, r, "restored", l, true)
	if got := countKind(st, harness.KindUser); got != n+1 {
		t.Errorf("%d prompts shown after the restore of %d turns", got, n+1)
	}
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
	r.Restart()
	second := ca.seen(t, r, "restored again", l, true)
	fail(t, "second restore", enginetest.Reread(st, second), l)
	ca.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40})
	ca.seen(t, r, "turn ended", l, false)
}

// TestKnownFault_Crush_PagesOfALongSessionComeTenSecondsApart documents the
// fault Goose has too: a chat of more than 500 rows loads one page of 500
// at a time, ten seconds apart. Correct: the whole chat is there within a
// few seconds of the restore. The cause is the feed's `limit 500`
// (crush.yaml) with source/sqlite.go:97-100, where a database that has not
// changed is not queried for ten seconds (sqliteRecheck). This passes while
// the fault is present.
func TestKnownFault_Crush_PagesOfALongSessionComeTenSecondsApart(t *testing.T) {
	needSQLite(t)
	e := newEnv(t, time.Hour)
	l, r := crushAgent.start(t, e, sessiongen.Script{Seed: 32})
	r.Stop()
	turns := make([]sessiongen.Turn, 0, 130)
	for i := 0; i < 130; i++ {
		turns = append(turns, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	}
	crushAgent.add(t, l, turns...)
	r.Start()
	time.Sleep(5 * time.Second)
	if enginetest.Has(l.Truth.Last)(r.Read()) {
		t.Fatal("the whole chat of 520 rows is there after five seconds: the fault is fixed")
	}
	if n := countKind(r.Read(), harness.KindUser); n == 0 || n >= 130 {
		t.Errorf("%d prompts after five seconds, want the first page only", n)
	}
}

func TestCrushRestartsAtSeededPoints(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/lifecycle/helper_restart")
	if n := crushAgent.restartsAtSeededPoints(t, 73, 14, 4); n != 4 {
		t.Fatalf("%d restarts", n)
	}
}

func TestCrushConnectionDrop(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/lifecycle/connection_drop")
	crushAgent.reconnect(t)
}

func TestCrushSessionRemoved(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/lifecycle/session_removed")
	crushAgent.removed(t)
}

func TestCrushConcurrentRequests(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/scale/concurrent_requests")
	crushAgent.hammer(t)
}

// The question tool: open while its dialog waits, answered, cancelled.
func TestCrushQuestions(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/capability/needs.questions")
	e := newEnv(t, time.Hour)
	l, r := crushAgent.start(t, e, sessiongen.Script{Seed: 33})
	ask := func(a *sessiongen.HookAsk, pending bool) sessiongen.Turn {
		return with(sessiongen.Turn{Say: 30}, func(h *sessiongen.HookTurn) { h.Ask, h.Pending = a, pending })
	}
	a := &sessiongen.HookAsk{Prompt: "Which colour?", Options: []string{"Red", "Green"}, Answer: "Green"}
	crushAgent.add(t, l, ask(a, true))
	st := crushAgent.seen(t, r, "open", l, true)
	q := the(t, st, harness.KindQuestion)
	if q.Question.Answered != nil || len(q.Question.Questions[0].Options) != 2 || !q.Question.Questions[0].AllowText || st.Mission.Summary.NeedsYou != 1 {
		t.Fatalf("open question %+v, summary %+v", q.Question, st.Mission.Summary)
	}
	crushAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 30})
	st = crushAgent.seen(t, r, "answered", l, false)
	if got := the(t, st, harness.KindQuestion).Question.Answered["0"]; len(got) != 1 || got[0] != "Green" || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("answered %v, summary %+v", got, st.Mission.Summary)
	}
	crushAgent.add(t, l, ask(&sessiongen.HookAsk{Prompt: "Skip it?", Options: []string{"a"}, Cancel: true}, false))
	st = crushAgent.seen(t, r, "cancelled", l, false)
	for _, q := range itemsOfKind(st, harness.KindQuestion) {
		if q.Question.Questions[0].Prompt == "Skip it?" && (q.Question.Answered == nil || len(q.Question.Answered["0"]) != 0) {
			t.Errorf("a cancelled question is %v, want closed with no answer", q.Question.Answered)
		}
	}
}

// A turn ends in a reply, a denied call's result, a failure the chat says,
// or the user's cancel.
func TestCrushTurnsEndAndInterrupt(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/lifecycle/turn_end", "crush/lifecycle/interrupt", "crush/capability/interrupt")
	e := newEnv(t, time.Hour)
	l, r := crushAgent.start(t, e, sessiongen.Script{Seed: 34})
	crushAgent.add(t, l, with(sessiongen.Turn{Say: 30, Tools: 2, Output: 50}, func(h *sessiongen.HookTurn) { h.Fail = 1 }))
	st := crushAgent.seen(t, r, "done", l, false)
	failedTools := 0
	for _, it := range itemsOfKind(st, harness.KindTool) {
		if it.Tool.State == harness.StateFailed {
			failedTools++
		}
	}
	if the(t, st, harness.KindTurn).Turn.State != harness.StateOK || failedTools != 1 {
		t.Errorf("turn %+v, %d failed tools", the(t, st, harness.KindTurn).Turn, failedTools)
	}
	crushAgent.add(t, l, sessiongen.Turn{Tools: 1, Output: 50, Interrupt: true})
	st = crushAgent.seen(t, r, "interrupted", l, false)
	interrupted := 0
	for _, it := range itemsOfKind(st, harness.KindTurn) {
		if it.Turn.State == harness.StateInterrupted {
			interrupted++
		}
	}
	if interrupted != 1 || st.Mission.Summary.Working {
		t.Errorf("%d interrupted turns, summary %+v", interrupted, st.Mission.Summary)
	}
	crushAgent.add(t, l, sessiongen.Turn{Tools: 1, Error: true})
	st = crushAgent.seen(t, r, "failed", l, false)
	var n *harness.NoticeBody
	for _, it := range notices(st) {
		n = it.Notice
	}
	if n == nil || n.Level != "error" || n.Reason != harness.ErrProviderUnreachable {
		t.Errorf("notice %+v, want the provider unreachable", n)
	}
}

func TestCrushExitAndRelaunch(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "crush/lifecycle/agent_exits", "crush/lifecycle/relaunch", "crush/lifecycle/resume", "crush/capability/resume")
	e := newEnv(t, time.Hour)
	l, r := crushAgent.start(t, e, sessiongen.Script{Seed: 35})
	crushAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50}, open(sessiongen.Turn{Say: 30, Tools: 2, Output: 50}))
	r.SetStatus("working")
	crushAgent.seen(t, r, "mid turn", l, true)
	st := r.SettleUntil(fileLimit, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	if !st.Mission.Summary.Working {
		t.Fatalf("summary %+v, want working", st.Mission.Summary)
	}
	r.SetEnded(true)
	gone := r.SettleUntil(fileLimit, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	for _, it := range gone.Items {
		if it.Tool != nil && running(it.Tool.State) {
			t.Errorf("tool %s is %s after the agent exited", it.ID, it.Tool.State)
		}
	}
	r.SetEnded(false)
	crushAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	st = r.SettleUntil(fileLimit, enginetest.Has(l.Truth.Last))
	if st.Mission.Summary.Working || len(itemsOfKind(st, harness.KindTurn)) < 3 {
		t.Errorf("%d turns, summary %+v after the relaunched turn", len(itemsOfKind(st, harness.KindTurn)), st.Mission.Summary)
	}
}

// TestKnownFault_Crush_NewConversationNotFollowed documents a limit the
// adapter names itself (crush.yaml, Attribution): a session Crush starts
// inside its own UI (ctrl+n) is not followed once the chat has content.
// Correct: the chat starts over on the new conversation. The cause is
// crush/session.go:131-133, where a session that has shown messages is kept.
// This passes while that holds.
func TestKnownFault_Crush_NewConversationNotFollowed(t *testing.T) {
	needSQLite(t)
	e := newEnv(t, time.Hour)
	l, r := crushAgent.start(t, e, sessiongen.Script{Seed: 36})
	crushAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	before := crushAgent.seen(t, r, "first", l, false)
	crushAgent.add(t, l, sessiongen.Turn{New: true, Say: 30, Tools: 1, Output: 50})
	time.Sleep(3 * time.Second)
	if enginetest.Has(l.Truth.Last)(r.Read()) {
		t.Fatal("the new conversation is shown: the fault is fixed; cover crush/lifecycle/new_conversation")
	}
	if r.Read().Epoch != before.Epoch {
		t.Errorf("the epoch changed")
	}
}
