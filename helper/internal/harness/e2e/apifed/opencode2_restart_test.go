package apifed

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// pastOnly drops the violations TestKnownFault_OpenCode2_RestartDropsTurnsAndSubagents
// stands for: a restarted 2.x chat has no turn or subagent items for what
// happened before the restart, nor the steps inside the subagents.
func pastOnly(vs []enginetest.Violation) []enginetest.Violation {
	var out []enginetest.Violation
	for _, v := range vs {
		d := v.Detail
		if v.Rule == "restart-lost-item" && (strings.HasPrefix(d, "turn-") || strings.HasPrefix(d, "agent-") || strings.Contains(d, "|agent-")) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// TestOpenCode2HelperRestart: the helper restarts at several points of a
// 2.x session, as `togo update` does while the agent runs: between turns,
// in the middle of a turn, and stopped for a while as the agent works on.
// The restarted helper finds the agent's server it started (its record
// under TOGO_HOME/run), reads the conversation's past through the paged
// messages route, and shows what the agent did while it was down: the
// prompts, the replies and a background shell started meanwhile. Apart
// from the known fault, the chat after a restart is the chat before it.
// Long mode crosses the history's 2,000-record bound: the chat says the
// earlier messages are in the terminal.
func TestOpenCode2HelperRestart(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/lifecycle/helper_restart")
	n := support.Scale(60, 560)
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 20, Tools: 1, Output: 20}
	}
	s := oc2Start(t, 61, nil, turns...)
	port := s.r.ServerPort()
	live := support.SettleUntil(s.r, n)
	support.CheckEdge(t, "start", live, s.l)

	// Between turns.
	s.r.Restart()
	after := support.SettleUntil(s.r, n)
	support.CheckEdge(t, "restart between turns", after, s.l)
	support.Fail(t, "restart between turns", pastOnly(support.Restarted(live, after)))
	if s.r.ServerPort() != port || len(s.api.Posts()) != 0 {
		t.Errorf("the restart did not find the server again: port %d then %d, calls %+v", port, s.r.ServerPort(), s.api.Posts())
	}
	paged := false
	for _, c := range s.historyReads() {
		paged = paged || strings.Contains(c.Path, "cursor=") && !strings.Contains(c.Path, "order=")
	}
	if !paged {
		t.Errorf("the history was not read past its first page: %+v", s.historyReads())
	}
	if support.LongMode() != noticeWith(after, "Earlier messages are not shown here") {
		t.Errorf("long mode %v, notice of earlier messages %v", support.LongMode(), noticeWith(after, "Earlier messages"))
	}

	// In the middle of a turn: the turn goes on, its reply is shown.
	var msg string
	s.do(t, func() (err error) { msg, err = s.p.Hold(s.l, "Held over a restart"); return err })
	support.WaitFor(t, s.r, "the held turn", func(st enginetest.State) bool { return st.Mission.Summary.Working })
	s.r.Restart()
	support.WaitFor(t, s.r, "the held prompt after the restart", shown("Held over a restart"))
	s.do(t, func() error { return s.p.Finish(s.l, msg) })
	st := support.WaitFor(t, s.r, "the held turn's reply", func(st enginetest.State) bool {
		_, ok := latestByID(st)[s.l.Truth.Last]
		return ok && !st.Mission.Summary.Working
	})
	// The held prompt is left with no reply in its turn: see
	// TestKnownFault_OpenCode2_RestartMidTurnLosesTheTurn.
	var vs []enginetest.Violation
	for _, v := range enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: s.l.Truth, Over: true}), support.Known...) {
		if v.Rule != "prompt-unanswered" {
			vs = append(vs, v)
		}
	}
	support.Fail(t, "restart mid-turn", vs)

	// Down while the agent works: three turns and a shell left running.
	s.r.Stop()
	s.add(t, sessiongen.Turn{Prompt: "While down 1", Say: 20, Tools: 1}, sessiongen.Turn{Prompt: "While down 2", Say: 20},
		sessiongen.Turn{Prompt: "While down 3", Say: 20, Jobs: 1})
	s.r.Start()
	st = support.WaitFor(t, s.r, "what the agent did while the helper was down", func(st enginetest.State) bool {
		_, ok := latestByID(st)[s.l.Truth.Last]
		return ok && shown("While down 1")(st) && shown("While down 3")(st) && len(rows(st, harness.SectionBackground)) == 1
	})
	support.CheckEdge(t, "after the helper was down", st, s.l)
	if s.r.ServerPort() != port {
		t.Errorf("a new server was started: port %d, was %d", s.r.ServerPort(), port)
	}
	before := st
	s.r.Restart()
	again := support.WaitFor(t, s.r, "the same chat after one more restart", func(st enginetest.State) bool {
		return len(support.UserPrompts(st)) == len(support.UserPrompts(before)) && len(rows(st, harness.SectionBackground)) == 1
	})
	support.Fail(t, "restore twice", pastOnly(support.Restarted(before, again)))
}

// TestKnownFault_OpenCode2_RestartDropsTurnsAndSubagents: after a helper
// restart, a 2.x chat shows the prompts, replies, reasoning and tool calls
// of the past, but none of its turns (their state, time and step count)
// and none of its subagents, nor what the subagents did.
//
// Cause: the past is read from the main conversation's messages route
// alone, and each message maps to its text and tool items only
// (adapters/opencode/history2.go:46-112: a user message sets pastTurn and
// makes no turn item; child sessions' messages are not read). Turn items
// come only from session.execution.* and agent items from
// session.created on the live stream (events2.go:349-370, 392-458).
//
// Correct: the chat after a restart is the chat before it, turns and
// subagents included (rule 2: the helper restarts, plan for it); the
// turns could be rebuilt from the user and idle messages, the subagents
// from the children route and their own messages.
func TestKnownFault_OpenCode2_RestartDropsTurnsAndSubagents(t *testing.T) {
	t.Parallel()
	s := oc2Start(t, 62, nil, sessiongen.Turn{Say: 40, Tools: 1}, sessiongen.Turn{Say: 30, Agents: &sessiongen.Batch{Size: 2, Steps: 1}})
	live := support.SettleUntil(s.r, 2)
	count := func(st enginetest.State) (turns, agents, inChild int) {
		for _, it := range latestByID(st) {
			switch {
			case it.Turn != nil && it.Parent == "":
				turns++
			case it.Agent != nil:
				agents++
			case strings.HasPrefix(it.Parent, "agent-"):
				inChild++
			}
		}
		return
	}
	t1, a1, c1 := count(live)
	if t1 != 2 || a1 != 2 || c1 == 0 {
		t.Fatalf("before the restart: %d turns, %d subagents, %d subagent items", t1, a1, c1)
	}
	s.r.Restart()
	after := support.SettleUntil(s.r, 2)
	t2, a2, c2 := count(after)
	if t2 != 0 || a2 != 0 || c2 != 0 {
		t.Fatalf("after the restart: %d turns, %d subagents, %d subagent items: the fault changed or is fixed; claim the items in TestOpenCode2HelperRestart", t2, a2, c2)
	}
	if len(support.UserPrompts(after)) != 2 {
		t.Fatalf("the prompts are gone too: %d", len(support.UserPrompts(after)))
	}
}

// TestKnownFault_OpenCode2_RestartMidTurnLosesTheTurn: when the helper
// restarts while a 2.x turn runs, what the agent does for the rest of the
// turn is shown outside it: the reply has no turn, so the prompt reads as
// unanswered and the turn never ends in the chat.
//
// Cause: the restarted mapper learns of the turn only from the past
// (history2.go:62, pastTurn, which the replay mapper keeps), and the live
// mapper's turn for the session (turnOf, state "turn") is set only by
// session.execution.started (events2.go:392-412), which came before the
// restart. Later text and tool events take turnFor(session), which is ""
// (events2.go:414-422, parts2.go).
//
// Correct: a turn that runs across a restart keeps its items: the
// mapper takes the newest user message without an idle marker after it as
// the running turn.
func TestKnownFault_OpenCode2_RestartMidTurnLosesTheTurn(t *testing.T) {
	t.Parallel()
	s := oc2Start(t, 63, nil, sessiongen.Turn{Say: 40, Tools: 1})
	support.SettleUntil(s.r, 1)
	var msg string
	s.do(t, func() (err error) { msg, err = s.p.Hold(s.l, "Held over a restart"); return err })
	support.WaitFor(t, s.r, "the held turn", func(st enginetest.State) bool { return st.Mission.Summary.Working })
	s.r.Restart()
	support.WaitFor(t, s.r, "the held prompt after the restart", shown("Held over a restart"))
	s.do(t, func() error { return s.p.Finish(s.l, msg) })
	st := support.WaitFor(t, s.r, "the reply", func(st enginetest.State) bool {
		_, ok := latestByID(st)[s.l.Truth.Last]
		return ok
	})
	reply := latestByID(st)[s.l.Truth.Last]
	var prompt harness.Item
	for _, it := range support.UserPrompts(st) {
		if it.User.Text == "Held over a restart" {
			prompt = it
		}
	}
	if reply.TurnID != "" || prompt.TurnID == "" {
		t.Fatalf("the reply is in turn %q, the prompt in %q: the fault changed or is fixed", reply.TurnID, prompt.TurnID)
	}
}
