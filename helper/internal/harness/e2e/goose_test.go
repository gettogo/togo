package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

var gooseAgent = fileAgent{name: "goose", profile: func() sessiongen.Profile { return &sessiongen.Goose{Mode: "approve"} }, cont: sessiongen.Continue}

// needSQLite skips a scenario on a machine with no sqlite3 program, which
// the helper itself needs to read these databases.
func needSQLite(t *testing.T) {
	t.Helper()
	if sessiongen.SQLiteBin() == "" {
		t.Skip("no sqlite3 program")
	}
	t.Parallel()
}

// A session past the feed's page of 200 rows is read page by page, each
// page ten seconds after the one before (see
// TestKnownFault_Goose_PagesOfALongSessionComeTenSecondsApart), so this
// one is 230 rows, and TOGO_E2E_LONG=1 makes it 1,500. The helper restores
// it, restarts, and shows the same chat.
func TestGooseLongSession(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/scale/long_session", "goose/capability/chat", "goose/capability/chat.history", "goose/scale/at_limit")
	n := 46
	if longRun() {
		n = 300
	}
	ga := gooseAgent
	ga.limit = time.Duration(10*(n/40)+30) * time.Second
	e := newEnv(t, 48*time.Hour)
	l, r := ga.start(t, e, sessiongen.Script{Seed: 21})
	r.Stop()
	turns := make([]sessiongen.Turn, 0, n+1)
	for i := 0; i < n; i++ {
		turns = append(turns, sessiongen.Turn{Say: 300, Tools: 1, Output: 1500})
	}
	ga.add(t, l, append(turns, open(sessiongen.Turn{Say: 50, Tools: 1, Output: 200}))...)
	r.Start()
	st := ga.seen(t, r, "restored", l, true)
	if got := countKind(st, harness.KindUser); got != n+1 {
		t.Errorf("%d prompts shown after the restore of %d turns", got, n+1)
	}
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
	r.Restart()
	second := ga.seen(t, r, "restored again", l, true)
	fail(t, "second restore", enginetest.Reread(st, second), l)
	ga.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40})
	ga.seen(t, r, "turn ended", l, false)
}

// TestKnownFault_Goose_PagesOfALongSessionComeTenSecondsApart documents a
// fault: a Goose chat of more than 200 messages loads one page of 200 rows
// at a time, ten seconds apart. Correct: the whole chat is there within a
// few seconds of the restore. The messages feed asks for `limit 200` rows
// after its cursor (goose.yaml), and a poll of a database that has not
// changed is skipped for ten seconds (source/sqlite.go:97-100,
// sqliteRecheck), which includes the poll that would read the next page.
// This passes while the fault is present.
func TestKnownFault_Goose_PagesOfALongSessionComeTenSecondsApart(t *testing.T) {
	needSQLite(t)
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 28})
	r.Stop()
	turns := make([]sessiongen.Turn, 0, 90)
	for i := 0; i < 90; i++ {
		turns = append(turns, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	}
	gooseAgent.add(t, l, turns...)
	r.Start()
	time.Sleep(5 * time.Second)
	if enginetest.Has(l.Truth.Last)(r.Read()) {
		t.Fatal("the whole chat of 450 messages is there after five seconds: the fault is fixed")
	}
	if n := countKind(r.Read(), harness.KindUser); n == 0 || n >= 90 {
		t.Errorf("%d prompts after five seconds, want the first page only", n)
	}
}

func TestGooseRestartsAtSeededPoints(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/lifecycle/helper_restart")
	if n := gooseAgent.restartsAtSeededPoints(t, 72, 14, 4); n != 4 {
		t.Fatalf("%d restarts", n)
	}
}

func TestGooseConnectionDrop(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/lifecycle/connection_drop")
	gooseAgent.reconnect(t)
}

func TestGooseSessionRemoved(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/lifecycle/session_removed")
	gooseAgent.removed(t)
}

func TestGooseConcurrentRequests(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/scale/concurrent_requests")
	gooseAgent.hammer(t)
}

// The session row's goose_mode is the mode Togo shows.
func TestGooseMode(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/capability/mode")
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 22, Turns: []sessiongen.Turn{{Say: 30}}})
	gooseAgent.seen(t, r, "chat", l, false)
	d, err := r.Engine.Describe(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "approve" {
		t.Errorf("mode %q, want approve (the session row's goose_mode)", d.Mode)
	}
}

// A tool asks for permission and waits; it is allowed, and the next is denied.
func TestGooseApprovals(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/capability/needs.approvals")
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 23})
	gooseAgent.add(t, l, open(with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve = 1 })))
	st := gooseAgent.seen(t, r, "waiting", l, true)
	q := the(t, st, harness.KindQuestion)
	if q.Question.Answered != nil || q.Question.Approve != "Allow" || q.Question.Deny != "Deny" || len(q.Question.Questions[0].Options) != 4 || st.Mission.Summary.NeedsYou != 1 {
		t.Fatalf("question %+v, summary %+v", q.Question, st.Mission.Summary)
	}
	gooseAgent.add(t, l, sessiongen.Turn{Prompt: sessiongen.Continue, Say: 30})
	st = gooseAgent.seen(t, r, "allowed", l, false)
	if a := the(t, st, harness.KindQuestion).Question.Answered["0"]; len(a) != 1 || a[0] != "Allow" || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("answered %v, summary %+v", a, st.Mission.Summary)
	}
	gooseAgent.add(t, l, with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve, h.Fail = 1, 1 }))
	st = gooseAgent.seen(t, r, "denied", l, false)
	var answers []string
	failed := 0
	for _, q := range itemsOfKind(st, harness.KindQuestion) {
		answers = append(answers, q.Question.Answered["0"]...)
	}
	for _, it := range itemsOfKind(st, harness.KindTool) {
		if it.Tool.State == harness.StateFailed {
			failed++
		}
	}
	if len(answers) != 2 || failed != 1 {
		t.Errorf("answers %v, %d failed tools; want Allow and Deny, one failed tool", answers, failed)
	}
	// A confirmation left open when the next prompt comes closes.
	gooseAgent.add(t, l, open(with(sessiongen.Turn{Say: 30, Tools: 1}, func(h *sessiongen.HookTurn) { h.Approve = 1 })))
	gooseAgent.seen(t, r, "waiting again", l, true)
	gooseAgent.add(t, l, sessiongen.Turn{Prompt: "next", Say: 20})
	st = gooseAgent.seen(t, r, "next turn", l, false)
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("an unanswered confirmation stays open: %+v", st.Mission.Summary)
	}
}

// An MCP tool's question reaches the database once answered.
func TestGooseQuestions(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/capability/needs.questions")
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 24})
	a := &sessiongen.HookAsk{Prompt: "Which colour should the banner use?", Options: []string{"red", "green", "blue"}, Answer: "green"}
	gooseAgent.add(t, l, with(sessiongen.Turn{Say: 30}, func(h *sessiongen.HookTurn) { h.Ask = a }))
	st := gooseAgent.seen(t, r, "answered", l, false)
	q := the(t, st, harness.KindQuestion)
	if got := q.Question.Answered["colour"]; len(got) != 1 || got[0] != "green" || len(q.Question.Questions[0].Options) != 3 || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("question %+v, summary %+v", q.Question, st.Mission.Summary)
	}
}

// A turn ends in a reply or in an error the chat says.
func TestGooseTurnsEnd(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/lifecycle/turn_end")
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 25})
	gooseAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	st := gooseAgent.seen(t, r, "done", l, false)
	if the(t, st, harness.KindTurn).Turn.State != harness.StateOK || st.Mission.Summary.Working {
		t.Fatalf("summary %+v", st.Mission.Summary)
	}
	gooseAgent.add(t, l, sessiongen.Turn{Tools: 1, Error: true})
	st = gooseAgent.seen(t, r, "failed", l, false)
	var n *harness.NoticeBody
	failedTurns := 0
	for _, it := range notices(st) {
		n = it.Notice
	}
	for _, it := range itemsOfKind(st, harness.KindTurn) {
		if it.Turn.State == harness.StateFailed {
			failedTurns++
		}
	}
	if n == nil || n.Level != "error" || failedTurns != 1 {
		t.Errorf("notice %+v, %d failed turns", n, failedTurns)
	}
}

// TestKnownFault_Goose_InterruptLeavesTheTurnRunning documents a fault: when
// the user interrupts a tool call, Goose stores only its failed result
// ("Tool call was interrupted before completing", approvals.jsonl row 104)
// and no reply, and the chat keeps the turn running and the summary working
// until the next prompt, which then closes the turn as ok. Correct: the turn
// ends interrupted. The cause is goose/session.go:283-288 and :338-349:
// a tool response ends only the tool, and only a reply or an error part ends
// the turn. This passes while the fault is present.
func TestKnownFault_Goose_InterruptLeavesTheTurnRunning(t *testing.T) {
	needSQLite(t)
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 29})
	gooseAgent.add(t, l, sessiongen.Turn{Tools: 1, Output: 50, Interrupt: true})
	st := r.SettleUntil(fileLimit, enginetest.Has(l.Truth.Last))
	for _, it := range itemsOfKind(st, harness.KindTool) {
		if it.Tool.State != harness.StateFailed {
			t.Fatalf("the interrupted call is %s, want failed", it.Tool.State)
		}
	}
	if turn := the(t, st, harness.KindTurn); turn.Turn.State != harness.StateRunning || !st.Mission.Summary.Working {
		t.Fatalf("turn %s, summary %+v: the fault is fixed; cover goose/lifecycle/interrupt", turn.Turn.State, st.Mission.Summary)
	}
}

// Goose exits in the middle of a turn: nothing is left running; started
// again with --session-id, the same session goes on.
func TestGooseExitAndRelaunch(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/lifecycle/agent_exits", "goose/lifecycle/relaunch", "goose/lifecycle/resume", "goose/capability/resume")
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 26})
	gooseAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50}, open(sessiongen.Turn{Say: 30, Tools: 2, Output: 50}))
	r.SetStatus("working")
	gooseAgent.seen(t, r, "mid turn", l, true)
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
	gooseAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	st = r.SettleUntil(fileLimit, enginetest.Has(l.Truth.Last))
	if st.Mission.Summary.Working || len(itemsOfKind(st, harness.KindTurn)) < 3 {
		t.Errorf("%d turns, summary %+v after the relaunched turn", len(itemsOfKind(st, harness.KindTurn)), st.Mission.Summary)
	}
}

// TestKnownFault_Goose_NewConversationNotFollowed documents a limit the
// adapter names itself (goose.yaml, Attribution): a session Goose starts
// inside its own UI (a new row in sessions.db) is not followed once the chat
// has content; the chat stays on the old one. Correct: the chat starts over
// on the new conversation. The cause is goose/session.go:157-159, where a
// session that has shown messages is kept. This passes while that holds.
func TestKnownFault_Goose_NewConversationNotFollowed(t *testing.T) {
	needSQLite(t)
	e := newEnv(t, time.Hour)
	l, r := gooseAgent.start(t, e, sessiongen.Script{Seed: 27})
	gooseAgent.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	before := gooseAgent.seen(t, r, "first", l, false)
	gooseAgent.add(t, l, sessiongen.Turn{New: true, Say: 30, Tools: 1, Output: 50})
	time.Sleep(3 * time.Second)
	if enginetest.Has(l.Truth.Last)(r.Read()) {
		t.Fatal("the new conversation is shown: the fault is fixed; cover goose/lifecycle/new_conversation")
	}
	if r.Read().Epoch != before.Epoch {
		t.Errorf("the epoch changed")
	}
}
