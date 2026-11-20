package apifed

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
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

// TestKnownFault_OpenCode_ContextPercentIsNeverShown documents a fault:
// opencode.yaml and kilo.yaml declare status.context, but the status block
// of a session never has a context percent. The assistant message the agent
// streams carries the turn's tokens (events.sse), and the model's context
// limit is in /config/providers, but no mapper reads either: nothing under
// adapters/opencode builds a StatusBlock with ContextPercent (Claude's and
// Codex's mappers do, claude/usage.go and codex/live.go), so the app's
// context gauge stays empty for these agents.
//
// Correct: after a turn that used 41,000 tokens of the model's window, the
// status block says how full the context is. This passes while the fault is
// present; when it fails the fault is fixed: write the test for the
// percentage and claim opencode/capability/status.context and
// kilo/capability/status.context.
func TestKnownFault_OpenCode_ContextPercentIsNeverShown(t *testing.T) {
	t.Parallel()
	for _, agent := range []string{"opencode", "kilo"} {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			s := apiStart(t, agent, 99, nil, sessiongen.Turn{Say: 20, Tools: 1}, sessiongen.Turn{Say: 20})
			support.SettleUntil(s.r, 2)
			// An assistant message that used 41,000 tokens, as the
			// stream carries it (adapters/opencode/testdata/events.sse),
			// written as the agent writes it.
			now := time.Now().UnixMilli()
			line := fmt.Sprintf(`{"type":"message.updated","properties":{"sessionID":%q,"info":{"id":"msg_tokens","role":"assistant",`+
				`"modelID":"m","providerID":"p","sessionID":%q,"time":{"created":%d,"completed":%d},"cost":0,`+
				`"tokens":{"input":41000,"output":900,"reasoning":0,"cache":{"read":0,"write":0}}}}}`+"\n", s.p.Session(), s.p.Session(), now, now)
			f, err := os.OpenFile(s.api.Journal(), os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(line); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			// The journal is read in order: when the next turn is shown,
			// the message before it has been read.
			s.add(t, sessiongen.Turn{Say: 20})
			st := support.SettleUntil(s.r, 3)
			if got := support.StatusOf(st); got.ContextPercent != nil {
				t.Fatalf("the status shows the context at %v percent: the fault is fixed", *got.ContextPercent)
			}
		})
	}
}
