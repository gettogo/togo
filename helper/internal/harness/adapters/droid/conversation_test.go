package droid

import "testing"

// A real capture of two Droid sessions run one after the other in one
// folder. The hooks' session id names the conversation from the session's
// start, so the second session makes the engine start the chat over; the
// first session's own late hooks name nothing new.
func TestHookSessionNamesTheConversation(t *testing.T) {
	const first, second = "64984b51-4432-4c4c-8479-0c4513298083", "7acb2434-9d75-42f4-89da-3ff2ca4c0d6d"
	got := conversationAfter(t, hookLines(t, "hooks-real-exec.jsonl"))
	want := []string{first, first, first, second, second, second, second, second}
	if len(got) != len(want) {
		t.Fatalf("conversations %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("after hook %d: conversation %q, want %q (all: %q)", i+1, got[i], want[i], got)
		}
	}
	// The same capture read again (the hook store after a helper restart)
	// names the same conversations in the same order.
	again := conversationAfter(t, hookLines(t, "hooks-real-exec.jsonl"))
	if again[len(again)-1] != second {
		t.Fatalf("replay ends on %q", again[len(again)-1])
	}
}
