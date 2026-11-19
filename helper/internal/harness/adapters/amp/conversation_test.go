package amp

import "testing"

// Togo's Amp plugin (helper/internal/hooks/amp.go) forwards only the
// thread, so a new thread and a nested `amp -x` run by the agent's shell
// tool look the same: the hooks name no conversation and the chat is
// never started over by a hook.
func TestSecondThreadDoesNotStartTheChatOver(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"session.start","payload":{"thread":{"id":"T-1"}}}`),
		[]byte(`{"event":"agent.start","payload":{"thread":{"id":"T-1"},"id":1,"message":"first"}}`),
		[]byte(`{"event":"session.start","payload":{"thread":{"id":"T-2"}}}`),
		[]byte(`{"event":"agent.start","payload":{"thread":{"id":"T-2"},"id":1,"message":"second"}}`),
	}
	for i, c := range conversationAfter(t, lines) {
		if c != "" {
			t.Fatalf("after event %d: conversation %q named; a nested run would start the chat over", i+1, c)
		}
	}
}
