package droid

import "testing"

// A real capture of two `droid exec` runs in one folder: the shape a
// nested run has when the agent's shell tool runs `droid exec` inside a
// Togo session. Its hooks say "startup" like a /new would, and nothing in
// the payload tells them apart, so the hooks name no conversation and the
// chat is never started over by a hook: keeping the history beats wiping
// it for a nested run.
func TestSecondSessionDoesNotStartTheChatOver(t *testing.T) {
	for i, c := range conversationAfter(t, hookLines(t, "hooks-real-exec.jsonl")) {
		if c != "" {
			t.Fatalf("after hook %d: conversation %q named; a nested run would start the chat over", i+1, c)
		}
	}
}
