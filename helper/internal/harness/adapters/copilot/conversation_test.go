package copilot

import (
	"strings"
	"testing"
)

// A real capture in which Copilot CLI started a second session (/new)
// after the first. The hooks' session id names the conversation from each
// session's start, so the second makes the engine start the chat over; a
// late hook of the session left does not move it back.
func TestHookSessionNamesTheConversation(t *testing.T) {
	const first, second = "424d4b08-6a27-4205-ac21-601540fc1edd", "718001ae-c1a9-4e71-b598-6fd61272a6af"
	lines := hookLines(t, "hooks-ask.jsonl")
	var firstLine []byte
	for _, l := range lines {
		if strings.Contains(string(l), first) {
			firstLine = l // a late hook of the first session
		}
	}
	got := conversationAfter(t, append(lines, firstLine))
	cur := ""
	for i, l := range lines {
		s := string(l)
		if strings.Contains(s, `"sessionStart"`) || strings.Contains(s, `"userPromptSubmitted"`) {
			cur = first
			if strings.Contains(s, second) {
				cur = second
			}
		}
		if got[i] != cur {
			t.Fatalf("after hook %d: conversation %q, want %q", i+1, got[i], cur)
		}
	}
	if cur != second {
		t.Fatal("the capture never moved to the second session")
	}
	if c := got[len(got)-1]; c != second {
		t.Fatalf("a late hook of the session left moved the chat back to %q", c)
	}
}
