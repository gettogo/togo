package amp

import "testing"

// Togo's Amp plugin (helper/internal/hooks/amp.go) forwards the thread
// with every event. The thread names the conversation from its start; a
// new thread (/new, or a thread opened from the list) moves it; a late
// event of the thread left does not move it back.
func TestThreadNamesTheConversation(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"session.start","payload":{"thread":{"id":"T-1"}}}`),
		[]byte(`{"event":"agent.start","payload":{"thread":{"id":"T-1"},"id":1,"message":"first"}}`),
		[]byte(`{"event":"agent.start","payload":{"thread":{"id":"T-2"},"id":1,"message":"second"}}`),
		[]byte(`{"event":"agent.end","payload":{"thread":{"id":"T-1"},"id":1,"status":"done"}}`),
	}
	got := conversationAfter(t, lines)
	want := []string{"T-1", "T-1", "T-2", "T-2"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("conversations %q, want %q", got, want)
		}
	}
}
