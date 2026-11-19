package e2e

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Amp's plugin forwards four events. A tool.result is the whole call, so
// a tool step has no running state in the chat.
var ampAgent = hookAgent{
	name:    "amp",
	profile: func() sessiongen.Profile { return &sessiongen.Amp{} },
	classify: func(c sessiongen.HookCall) []kind {
		switch c.Event {
		case "agent.start":
			return []kind{{"prompt", c.Payload["message"].(string)}}
		case "tool.result":
			return []kind{{what: "tool"}}
		case "agent.end":
			if c.Payload["last"] != "" {
				return []kind{{what: "reply"}}
			}
		}
		return nil
	},
	replies: true, startEvent: "session.start", long: 480,
}

func TestAmpLongSession(t *testing.T) {
	coverage.Covers(t, "generated", "amp/scale/long_session", "amp/capability/chat")
	n := ampAgent.long
	if longRun() {
		n = 2000
	}
	longScenario(t, ampAgent, n)
}

func TestAmpRestartsAtSeededPoints(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/helper_restart")
	if kinds := restartScenario(t, ampAgent); len(kinds) != 5 {
		t.Fatalf("restart points %v", kinds)
	}
}

// TestKnownFault_Amp_RestartDropsTheChat documents a fault: a restart of
// the helper empties the chat of a hook-driven session. Correct: the chat
// read so far is still there. Amp keeps its threads on its own servers
// and writes nothing locally (amp/amp.yaml), so the helper's memory is the
// only copy. The cause is source/hook.go:104-111 (the hook bus keeps a
// session's last 64 payloads in memory) with engine/engine.go:202. This
// passes while the fault is present.
func TestKnownFault_Amp_RestartDropsTheChat(t *testing.T) {
	before, after := restartDropsChat(t, ampAgent)
	if before == 0 {
		t.Fatal("nothing was drawn before the restart")
	}
	if after != 0 {
		t.Fatalf("the chat survived the restart (%d of %d items): the fault is fixed", after, before)
	}
}

// A new thread (the agent's new conversation) in the same session.
func TestAmpNewConversation(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/new_conversation")
	newConversation(t, ampAgent, false)
}

func TestAmpExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/agent_exits", "amp/lifecycle/relaunch")
	exitAndRelaunch(t, ampAgent)
}

// The plugin's agent.end says cancelled.
func TestAmpInterruptMidTurn(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/interrupt", "amp/lifecycle/turn_end")
	interrupted(t, ampAgent)
}

func TestAmpHooksOutOfOrderDuplicatedAndMissing(t *testing.T) {
	hookOrder(t, ampAgent)
}

func TestAmpConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/connection_drop")
	connectionDrop(t, ampAgent)
}

func TestAmpSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "amp/lifecycle/session_removed")
	sessionRemoved(t, ampAgent)
}

func TestAmpAtLimit(t *testing.T) {
	coverage.Covers(t, "generated", "amp/scale/at_limit")
	atLimit(t, ampAgent)
}

func TestAmpConcurrentRequests(t *testing.T) {
	coverage.Covers(t, "generated", "amp/scale/concurrent_requests")
	concurrent(t, ampAgent)
}
