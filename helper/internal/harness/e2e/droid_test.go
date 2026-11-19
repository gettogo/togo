package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

var droidAgent = hookAgent{
	name:    "droid",
	profile: func() sessiongen.Profile { return &sessiongen.Droid{} },
	classify: func(c sessiongen.HookCall) []kind {
		switch c.Event {
		case "UserPromptSubmit":
			return []kind{{"prompt", c.Payload["prompt"].(string)}}
		case "PreToolUse":
			if n := c.Payload["tool_name"]; n == "AskUser" || n == "ExitSpecMode" {
				return []kind{{what: "ask"}}
			}
			return []kind{{what: "tool"}}
		}
		return nil
	},
	// Droid's Stop hook carries no reply and no mapper reads its
	// transcript's replies: see TestKnownFault_Droid_RepliesNotShown.
	replies: false, canAsk: true, startEvent: "SessionStart", long: 560,
}

func TestDroidLongSession(t *testing.T) {
	coverage.Covers(t, "generated", "droid/scale/long_session", "droid/capability/chat")
	n := droidAgent.long
	if longRun() {
		n = 2000
	}
	longScenario(t, droidAgent, n)
}

func TestDroidRestartsAtSeededPoints(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/helper_restart")
	if kinds := restartScenario(t, droidAgent); len(kinds) != 5 {
		t.Fatalf("restart points %v", kinds)
	}
}

// The chat read so far is still there after a helper restart (`togo
// update`): Droid's hooks are the only copy Togo reads.
func TestDroid_RestartKeepsTheChat(t *testing.T) {
	restartKeepsChat(t, droidAgent)
}

// TestKnownFault_Droid_RepliesNotShown documents a fault: the chat of a
// Droid session shows what the user typed and the tools, and never what
// Droid answered. Its Stop hook carries no reply text (droid/testdata/
// hooks-real-exec.jsonl) and nothing reads the reply from the session
// file, where it is (droid/droid.go: "No transcript mapper draws the chat
// yet"). Correct: each finished turn has its reply in the chat. The cause
// is hookstyle/mapper.go:280-286, where end() draws a reply only when the
// payload has last_assistant_message. This passes while the fault is
// present.
func TestKnownFault_Droid_RepliesNotShown(t *testing.T) {
	e := newEnv(t, time.Hour)
	l := write(t, e, droidAgent, []sessiongen.Turn{{Say: 40, Tools: 2, Output: 20}, {Say: 40, Tools: 1, Output: 20}})
	r := e.rig(t, "droid")
	run(t, r, droidAgent, l.Hooks)
	st := settled(r, lastPrompt(droidAgent, l.Hooks))
	replies, turns := 0, 0
	for _, it := range st.Items {
		if it.Assistant != nil {
			replies++
		}
		if it.Turn != nil && it.Turn.State == harness.StateOK {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("%d finished turns, want 2", turns)
	}
	if replies != 0 {
		t.Fatalf("%d replies in the chat: the fault is fixed; set replies on droidAgent and delete this test", replies)
	}
}

func TestDroidNewConversationAndResume(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/new_conversation", "droid/lifecycle/resume")
	st := newConversation(t, droidAgent, true)
	notes := 0
	for _, it := range st.Items {
		if it.Notice != nil && it.Notice.Code == harness.NoticeResumed {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d resume notices, want 1", notes)
	}
}

func TestDroidExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/agent_exits", "droid/lifecycle/relaunch")
	exitAndRelaunch(t, droidAgent)
}

// Droid fires no Stop on an abort: a Notification says it stopped.
func TestDroidInterruptMidTurn(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/interrupt", "droid/lifecycle/turn_end")
	interrupted(t, droidAgent)
}

func TestDroidHooksOutOfOrderDuplicatedAndMissing(t *testing.T) {
	hookOrder(t, droidAgent)
}

func TestDroidConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/connection_drop")
	connectionDrop(t, droidAgent)
}

func TestDroidSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "droid/lifecycle/session_removed")
	sessionRemoved(t, droidAgent)
}

func TestDroidAtLimit(t *testing.T) {
	coverage.Covers(t, "generated", "droid/scale/at_limit")
	atLimit(t, droidAgent)
}

func TestDroidConcurrentRequests(t *testing.T) {
	coverage.Covers(t, "generated", "droid/scale/concurrent_requests")
	concurrent(t, droidAgent)
}

// Droid's AskUser questionnaire and its spec approval (ExitSpecMode, with
// the plan drawn before the question).
func TestDroidQuestions(t *testing.T) {
	coverage.Covers(t, "generated", "droid/capability/needs.questions")
	t.Run("AskUser", func(t *testing.T) { questions(t, droidAgent, false) })
	t.Run("ExitSpecMode", func(t *testing.T) { questions(t, droidAgent, true) })
}
