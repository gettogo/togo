package claude

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The recorded hooks of one session, read as another session's: the same
// payloads under another session id and transcript.
func otherSession(t *testing.T, id string) []harness.Record {
	t.Helper()
	const own = "0b6e5f8e-1111-4222-8333-944455556666"
	var out []harness.Record
	for _, r := range records(t, "hooks.jsonl") {
		r.Data = []byte(strings.ReplaceAll(string(r.Data), own, id))
		out = append(out, r)
	}
	return out
}

// Seen in review: a `claude -p` run by the agent's shell tool inherits
// TOGO_SESSION and reports its hooks here; its SessionStart moved the
// transcript feed, so the chat started over on the nested run, then again
// on the parent's next prompt. A nested session never moves the chat and
// its hooks do not change this session's state. /clear, /resume and a
// relaunch after the session ended still move it.
func TestNestedClaudeRunDoesNotMoveTheChat(t *testing.T) {
	const own = "0b6e5f8e-1111-4222-8333-944455556666"
	h := newHooks(harness.Env{})
	for _, r := range records(t, "hooks.jsonl")[:2] { // SessionStart, UserPromptSubmit
		h.Map(r)
	}
	if st := h.State(); st["harness_session"] != own || st["working"] != "true" {
		t.Fatalf("own session not followed: %v", st)
	}
	for _, r := range otherSession(t, "1c7f6a9f-2222-4333-8444-a55566667777") {
		h.Map(r) // startup, prompt, tasks, stop of the nested run
	}
	st := h.State()
	if st["harness_session"] != own || !strings.Contains(st["transcript_path"], own) {
		t.Fatalf("the nested run moved the chat: %v", st)
	}
	if st["working"] != "true" {
		t.Fatal("the nested run's Stop ended the parent's turn")
	}
	clear := otherSession(t, "2d8a7b0a-3333-4444-8555-b66677778888")[0]
	clear.Data = []byte(strings.Replace(string(clear.Data), `"source":"startup"`, `"source":"clear"`, 1))
	h.Map(clear)
	if st := h.State(); st["harness_session"] != "2d8a7b0a-3333-4444-8555-b66677778888" {
		t.Fatalf("/clear did not move the chat: %v", st)
	}
	end := []byte(`{"session_id":"2d8a7b0a-3333-4444-8555-b66677778888","hook_event_name":"SessionEnd","reason":"prompt_input_exit"}`)
	h.Map(harness.Record{Data: end})
	relaunch := otherSession(t, "3e9b8c1b-4444-4555-8666-c77788889999")[0]
	h.Map(relaunch)
	if st := h.State(); st["harness_session"] != "3e9b8c1b-4444-4555-8666-c77788889999" {
		t.Fatalf("a start after the session ended did not move the chat: %v", st)
	}
}
