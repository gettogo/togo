package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

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
	coverage.Covers(t, "generated", "copilot/scale/long_session", "copilot/capability/chat", "copilot/capability/agents")
	n := copilotAgent.long
	if longRun() {
		n = 2000
	}
	longScenario(t, copilotAgent, n)
}

// The engine is restarted between turns, in the middle of one and while a
// permission waits; what the hooks draw afterwards is exact and nothing
// is left running.
func TestCopilotRestartsAtSeededPoints(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/lifecycle/helper_restart")
	kinds := restartScenario(t, copilotAgent)
	if len(kinds) != 5 {
		t.Fatalf("restart points %v", kinds)
	}
}

// TestKnownFault_Copilot_RestartDropsTheChat documents a fault: the
// engine keeps the chat of a hook-driven session in memory only and the
// hook bus keeps nothing for a new engine, so a restart (`togo update`)
// empties the chat while the agent goes on. Correct: the chat read so far
// is still there after the restart (omp and Claude Code read it back from
// their transcript; Copilot's events.jsonl holds the replies, and the
// prompts and tools are in no file Togo reads). The cause is
// source/hook.go:104-111 (the bus's backlog is a session's last 64
// payloads in memory) with engine/engine.go:202 (Forget drops it). This
// passes while the fault is present.
func TestKnownFault_Copilot_RestartDropsTheChat(t *testing.T) {
	before, after := restartDropsChat(t, copilotAgent)
	if before == 0 {
		t.Fatal("nothing was drawn before the restart")
	}
	if after != 0 {
		t.Fatalf("the chat survived the restart (%d of %d items): the fault is fixed; delete this test and the filter in restartScenario", after, before)
	}
}

// A second conversation (the agent's /new) and a resume of the first.
func TestCopilotNewConversationAndResume(t *testing.T) {
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
	coverage.Covers(t, "generated", "copilot/lifecycle/agent_exits", "copilot/lifecycle/relaunch")
	exitAndRelaunch(t, copilotAgent)
}

// The user interrupts a turn that has tools and subagents running.
func TestCopilotInterruptMidTurn(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/lifecycle/interrupt", "copilot/lifecycle/turn_end")
	interrupted(t, copilotAgent)
}

// Hooks run as separate processes: one can arrive twice, two can swap,
// one can be lost.
func TestCopilotHooksOutOfOrderDuplicatedAndMissing(t *testing.T) {
	hookOrder(t, copilotAgent)
}

func TestCopilotConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/lifecycle/connection_drop")
	connectionDrop(t, copilotAgent)
}

func TestCopilotSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/lifecycle/session_removed")
	sessionRemoved(t, copilotAgent)
}

func TestCopilotAtLimit(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/scale/at_limit")
	atLimit(t, copilotAgent)
}

// Copilot's ask_user: open while the agent waits, closed by an answer, a
// cancel or the next prompt.
func TestCopilotQuestions(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/capability/needs.questions")
	questions(t, copilotAgent, false)
}

// A tool that asks permission waits until the user answers: its row shows
// waiting while the agent waits, then runs and ends. A restart while it
// waits (TestCopilotRestartsAtSeededPoints) leaves nothing waiting.
func TestCopilotPermissionWaits(t *testing.T) {
	coverage.Covers(t, "generated", "copilot/capability/needs.approvals")
	e := newEnv(t, time.Hour)
	l := write(t, e, copilotAgent, []sessiongen.Turn{{Say: 20, Tools: 2, Hook: &sessiongen.HookTurn{Approve: 1}}})
	r := e.rig(t, "copilot")
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
	coverage.Covers(t, "generated", "copilot/scale/concurrent_requests")
	concurrent(t, copilotAgent)
}
