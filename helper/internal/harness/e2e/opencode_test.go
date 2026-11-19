package e2e

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

// OpenCode 1.x, served through the test API server (apiagent_test.go).
// OpenCode 2.x (opencode2.yaml) types its prompts and reads a history
// route: its scenarios are in opencode2_*_test.go, its cells in the
// opencode2 row.

func TestOpenCodeLongSession(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/scale/long_session", "opencode/capability/chat", "opencode/capability/agents",
		"opencode/capability/plan", "opencode/capability/changes")
	apiLong(t, "opencode")
}

func TestOpenCodeAtLimit(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/scale/at_limit")
	apiAtLimit(t, "opencode")
}

func TestOpenCodeNewAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/lifecycle/new_conversation", "opencode/lifecycle/resume",
		"opencode/capability/resume", "opencode/capability/send.api")
	apiNewAndResume(t, "opencode")
}

func TestOpenCodeNeeds(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/needs.approvals", "opencode/capability/needs.questions")
	apiNeeds(t, "opencode")
}

func TestOpenCodeSettings(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/model", "opencode/capability/mode", "opencode/capability/commands",
		"opencode/capability/compact", "opencode/capability/settings", "opencode/capability/status")
	apiSettings(t, "opencode")
}

func TestOpenCodeStream(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/chat.stream")
	apiStream(t, "opencode")
}

func TestOpenCodeQueue(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/send.queue")
	apiQueue(t, "opencode")
}

func TestOpenCodeErrors(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/send.api")
	apiErrors(t, "opencode")
}

func TestOpenCodeStreamDrop(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/lifecycle/connection_drop")
	apiStreamDrop(t, "opencode")
}

func TestOpenCodeServerMoves(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/lifecycle/relaunch")
	apiServerMoves(t, "opencode")
}

// TestKnownFault_OpenCode_RestartLosesChat: after a helper restart the
// chat of an OpenCode 1.x session is empty, and so are its plan, changes
// and subagents, until the agent's next event; what the agent did while
// the helper was down is never shown. The session goes on working: the
// next turn is shown and a prompt from the phone reaches the agent.
//
// Cause: the adapter reads only the event stream (opencode.yaml feeds:
// /event, no history), and the stream has no history: it sends what
// happens from the moment it is opened (source/httpsrc/sse.go Watch).
// The engine keeps no items across a restart.
//
// Correct: after a restart the chat shows the conversation as before it,
// read from the agent (OpenCode 1.x serves a session's messages, as the
// 2.x adapter reads them through its history route, opencode2.yaml).
func TestKnownFault_OpenCode_RestartLosesChat(t *testing.T) {
	t.Parallel()
	before, after := apiRestartLosesChat(t, "opencode")
	if before == 0 {
		t.Fatal("the session showed nothing before the restart")
	}
	if after != 0 {
		t.Fatalf("the chat holds %d of %d items after the restart: the fault is fixed, claim opencode/lifecycle/helper_restart", after, before)
	}
}
