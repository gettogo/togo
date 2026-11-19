package copilot

import "testing"

// A real capture with a second Copilot CLI session after the first. Both
// session starts say "new", which is also what a nested `copilot -p` run
// by the agent's shell tool says, so the hooks name no conversation and
// the chat is never started over by a hook: keeping the history beats
// wiping it for a nested run.
func TestSecondSessionDoesNotStartTheChatOver(t *testing.T) {
	for i, c := range conversationAfter(t, hookLines(t, "hooks-ask.jsonl")) {
		if c != "" {
			t.Fatalf("after hook %d: conversation %q named; a nested run would start the chat over", i+1, c)
		}
	}
}
