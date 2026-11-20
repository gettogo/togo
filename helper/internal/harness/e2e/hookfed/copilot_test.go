package hookfed

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The scenarios of the hook-driven agents (Copilot CLI, Droid, Cursor CLI,
// Amp) run side by side, except the four restart scenarios
// (TestXRestartsAtSeededPoints): run among the others they lose an
// approval or a tool's result across the restart in about one run of
// three, and they do not when they run alone. The lost approval is the
// engine's offer of a question answered while the offer opened, drawn as
// a card of its own, which no restart keeps
// (TestKnownFault_AskOfferAnsweredWhileOpeningShowsAsApproval); the
// restart check leaves it out. The lost tool's result is not traced.
// They take about a minute together.
var copilotAgent = hookAgent{
	name:    "copilot",
	profile: func() sessiongen.Profile { return &sessiongen.Copilot{} },
	classify: func(c sessiongen.HookCall) []kind {
		var out []kind
		switch c.Event {
		case "userPromptSubmitted":
			out = append(out, kind{"prompt", c.Payload["prompt"].(string)})
		case "preToolUse":
			if c.Payload["toolName"] == "ask_user" {
				out = append(out, kind{what: "ask"})
			} else {
				out = append(out, kind{what: "tool"})
			}
		}
		// The agent's messages are in its log: the chat shows each one
		// that has text.
		for _, w := range c.Writes {
			rec := w.Record.(map[string]any)
			if rec["type"] != "assistant.message" {
				continue
			}
			if d := rec["data"].(map[string]any); d["content"] != "" {
				out = append(out, kind{what: "reply"})
			}
		}
		return out
	},
	replies: true, canAsk: true, canApprove: true, canAgents: true,
	waitEvent: "permissionRequest", startEvent: "sessionStart", slowStop: true, long: 340,
}

// A session of hundreds of turns, as long as the chat can keep.
func TestCopilotLongSession(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "copilot/scale/long_session", "copilot/capability/chat")
	n := copilotAgent.long
	if support.LongMode() {
		n = 2000
	}
	longScenario(t, copilotAgent, n)
}

// Subagents show as rows until a stop hook or the turn's end closes them.
func TestCopilotSubagentRows(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/capability/agents")
	agentRows(t, copilotAgent)
}

// The engine is restarted between turns, in the middle of one and while a
// permission waits; what the hooks draw afterwards is exact and nothing
// is left running.
func TestCopilotRestartsAtSeededPoints(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/helper_restart")
	kinds := restartScenario(t, copilotAgent)
	if len(kinds) != 5 {
		t.Fatalf("restart points %v", kinds)
	}
}

// The chat read so far is still there after a helper restart (`togo
// update`): the prompts and tools exist only as hooks, and the replies are
// read again from Copilot's events.jsonl as far as each hook first read
// them.
func TestCopilot_RestartKeepsTheChat(t *testing.T) {
	t.Parallel()
	restartKeepsChat(t, copilotAgent)
}

// A second conversation (the agent's /new) and a resume of the first.
func TestCopilotNewConversationAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/new_conversation", "copilot/lifecycle/resume")
	st := newConversation(t, copilotAgent, true)
	notes := 0
	for _, it := range st.Items {
		if it.Notice != nil && it.Notice.Code == harness.NoticeResumed {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d resume notices, want 1 (a new conversation says nothing)", notes)
	}
}

// The agent exits with subagents running, then is started again.
func TestCopilotExitAndRelaunch(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/agent_exits", "copilot/lifecycle/relaunch")
	exitAndRelaunch(t, copilotAgent)
}

// The user interrupts a turn that has tools and subagents running.
func TestCopilotInterruptMidTurn(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/interrupt", "copilot/lifecycle/turn_end")
	interrupted(t, copilotAgent)
}

// Hooks run as separate processes: one can arrive twice, two can swap,
// one can be lost.
func TestCopilotHooksOutOfOrderDuplicatedAndMissing(t *testing.T) {
	t.Parallel()
	hookOrder(t, copilotAgent)
}

func TestCopilotConnectionDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/connection_drop")
	connectionDrop(t, copilotAgent)
}

func TestCopilotSessionRemoved(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/lifecycle/session_removed")
	sessionRemoved(t, copilotAgent)
}

func TestCopilotAtLimit(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "copilot/scale/at_limit")
	atLimit(t, copilotAgent)
}

// Copilot's ask_user: open while the agent waits, closed by an answer, a
// cancel or the next prompt.
func TestCopilotQuestions(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/capability/needs.questions")
	questions(t, copilotAgent, false)
}

// A tool that asks permission waits until the user answers: its row shows
// waiting while the agent waits, then runs and ends. A restart while it
// waits (TestCopilotRestartsAtSeededPoints) leaves nothing waiting.
func TestCopilotPermissionWaits(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/capability/needs.approvals")
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, copilotAgent, []sessiongen.Turn{{Say: 20, Tools: 2, Hook: &sessiongen.HookTurn{Approve: 1}}})
	r := e.Rig(t, "copilot")
	at := 0
	for i, c := range l.Hooks {
		if c.Event == "permissionRequest" {
			at = i + 1
		}
	}
	run(t, r, copilotAgent, l.Hooks[:at])
	waiting := r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.Tool != nil && it.Tool.State == harness.StateWaiting {
				return true
			}
		}
		return false
	})
	if !waiting.Mission.Summary.Working {
		t.Errorf("the summary says idle while a tool waits for permission")
	}
	run(t, r, copilotAgent, l.Hooks[at:])
	st := settled(r, lastPrompt(copilotAgent, l.Hooks))
	for _, it := range st.Items {
		if it.Tool != nil && it.Tool.State != harness.StateOK {
			t.Errorf("tool %s is %s after the permission was given", it.ID, it.Tool.State)
		}
	}
	checkHook(t, "permission", copilotAgent, st, l, true)
}

func TestCopilotConcurrentRequests(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/scale/concurrent_requests")
	concurrent(t, copilotAgent)
}
