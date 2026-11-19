package apifed

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

// Kilo Code, an OpenCode fork serving the same API and stream, through the
// test API server (apiagent_test.go).

func TestKiloLongSession(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/scale/long_session", "kilo/capability/chat", "kilo/capability/agents",
		"kilo/capability/plan", "kilo/capability/changes")
	apiLong(t, "kilo")
}

func TestKiloAtLimit(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/scale/at_limit")
	apiAtLimit(t, "kilo")
}

func TestKiloNewAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/lifecycle/new_conversation", "kilo/lifecycle/resume",
		"kilo/capability/resume", "kilo/capability/send.api")
	apiNewAndResume(t, "kilo")
}

func TestKiloNeeds(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/needs.approvals", "kilo/capability/needs.questions")
	apiNeeds(t, "kilo")
}

func TestKiloSettings(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/model", "kilo/capability/mode", "kilo/capability/commands",
		"kilo/capability/compact", "kilo/capability/settings", "kilo/capability/status")
	apiSettings(t, "kilo")
}

func TestKiloStream(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/chat.stream")
	apiStream(t, "kilo")
}

func TestKiloQueue(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/send.queue")
	apiQueue(t, "kilo")
}

func TestKiloErrors(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/send.api")
	apiErrors(t, "kilo")
}

func TestKiloStreamDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/lifecycle/connection_drop")
	apiStreamDrop(t, "kilo")
}

func TestKiloServerMoves(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/lifecycle/relaunch")
	apiServerMoves(t, "kilo")
}

// TestKnownFault_Kilo_RestartLosesChat: the fault of
// TestKnownFault_OpenCode_RestartLosesChat, for Kilo Code: after a helper
// restart the chat is empty until the agent's next event. Same cause: the
// adapter reads only the event stream (kilo.yaml feeds: /event, no
// history), which has no history.
//
// Correct: the chat shows the conversation as before the restart, read
// from the agent.
func TestKnownFault_Kilo_RestartLosesChat(t *testing.T) {
	t.Parallel()
	before, after := apiRestartLosesChat(t, "kilo")
	if before == 0 {
		t.Fatal("the session showed nothing before the restart")
	}
	if after != 0 {
		t.Fatalf("the chat holds %d of %d items after the restart: the fault is fixed, claim kilo/lifecycle/helper_restart", after, before)
	}
}
