package cursor

import (
	"strings"
	"testing"
)

// Cursor's hooks carry the chat's conversation_id. It names the
// conversation from the session's start; a new chat (a session start under
// another id) moves it.
func TestHookConversationIDNamesTheConversation(t *testing.T) {
	const first = "77af3c5f-9de2-4e24-bd72-f2891b981afd"
	lines := hookLines(t, "hooks-real-shell.jsonl")
	for i, c := range conversationAfter(t, lines) {
		if c != first {
			t.Fatalf("after hook %d: conversation %q, want %q", i+1, c, first)
		}
	}
	const other = "0b5d3a10-0000-4000-8000-0000000000aa"
	moved := append(append([][]byte(nil), lines...), []byte(strings.ReplaceAll(string(lines[0]), first, other)))
	got := conversationAfter(t, moved)
	if c := got[len(got)-1]; c != other {
		t.Fatalf("after a new chat's start: conversation %q, want %q", c, other)
	}
}
