package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

var cursorAgent = hookAgent{
	name:    "cursor",
	profile: func() sessiongen.Profile { return &sessiongen.Cursor{} },
	classify: func(c sessiongen.HookCall) []kind {
		switch c.Event {
		case "beforeSubmitPrompt":
			return []kind{{"prompt", c.Payload["prompt"].(string)}}
		case "preToolUse":
			return []kind{{what: "tool"}}
		case "afterAgentResponse":
			return []kind{{what: "reply"}}
		}
		return nil
	},
	replies: true, canAgents: true, startEvent: "sessionStart", long: 440,
}

func TestCursorLongSession(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/scale/long_session", "cursor/capability/chat")
	n := cursorAgent.long
	if longRun() {
		n = 2000
	}
	longScenario(t, cursorAgent, n)
}

func TestCursorSubagentRows(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/capability/agents")
	agentRows(t, cursorAgent)
}

// The model each prompt's payload names is the one the model picker shows
// as selected.
func TestCursorModelIsTheOneTheHooksName(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/capability/model")
	e := newEnv(t, time.Hour)
	l := write(t, e, cursorAgent, []sessiongen.Turn{
		{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Model: "gpt-5"}},
		{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Model: "composer-2.5"}},
	})
	r := e.rig(t, "cursor")
	selected := func() string {
		sc, err := r.Engine.Screen(context.Background(), enginetest.SessionID, "model", nil)
		if err != nil || sc.Picker == nil {
			t.Fatalf("model screen: %v %+v", err, sc)
		}
		return sc.Picker.Selected
	}
	var firstEnd int
	for i, c := range l.Hooks {
		if c.End != "" && firstEnd == 0 {
			firstEnd = i + 1
		}
	}
	run(t, r, cursorAgent, l.Hooks[:firstEnd])
	settled(r, lastPrompt(cursorAgent, l.Hooks[:firstEnd]))
	if got := selected(); got != "gpt-5" {
		t.Errorf("selected model %q after a turn on gpt-5", got)
	}
	run(t, r, cursorAgent, l.Hooks[firstEnd:])
	settled(r, lastPrompt(cursorAgent, l.Hooks))
	if got := selected(); got != "composer-2.5" {
		t.Errorf("selected model %q after a turn on composer-2.5", got)
	}
}

func TestCursorRestartsAtSeededPoints(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/helper_restart")
	if kinds := restartScenario(t, cursorAgent); len(kinds) != 5 {
		t.Fatalf("restart points %v", kinds)
	}
}

// TestKnownFault_Cursor_RestartDropsTheChat documents a fault: a restart
// of the helper empties the chat of a hook-driven session. Correct: the
// chat read so far is still there. The cause is source/hook.go:104-111
// (the hook bus keeps a session's last 64 payloads in memory) with
// engine/engine.go:202; Cursor's chat store is SQLite protobuf that no
// mapper reads (cursor/cursor.yaml header). This passes while the fault
// is present.
func TestKnownFault_Cursor_RestartDropsTheChat(t *testing.T) {
	before, after := restartDropsChat(t, cursorAgent)
	if before == 0 {
		t.Fatal("nothing was drawn before the restart")
	}
	if after != 0 {
		t.Fatalf("the chat survived the restart (%d of %d items): the fault is fixed", after, before)
	}
}

func TestCursorNewConversation(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/new_conversation")
	newConversation(t, cursorAgent, false)
}

func TestCursorExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/agent_exits", "cursor/lifecycle/relaunch")
	exitAndRelaunch(t, cursorAgent)
}

// The stop hook says aborted.
func TestCursorInterruptMidTurn(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/interrupt", "cursor/lifecycle/turn_end")
	interrupted(t, cursorAgent)
}

func TestCursorHooksOutOfOrderDuplicatedAndMissing(t *testing.T) {
	hookOrder(t, cursorAgent)
}

func TestCursorConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/connection_drop")
	connectionDrop(t, cursorAgent)
}

func TestCursorSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/lifecycle/session_removed")
	sessionRemoved(t, cursorAgent)
}

func TestCursorAtLimit(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/scale/at_limit")
	atLimit(t, cursorAgent)
}

func TestCursorConcurrentRequests(t *testing.T) {
	coverage.Covers(t, "generated", "cursor/scale/concurrent_requests")
	concurrent(t, cursorAgent)
}

