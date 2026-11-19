package cursor

import (
	"strings"
	"testing"
)

// Cursor's session start carries no source, so a new chat and a nested
// `cursor-agent -p` run by the agent's shell tool look the same: the hooks
// name no conversation and the chat is never started over by a hook.
func TestSecondSessionDoesNotStartTheChatOver(t *testing.T) {
	const first = "77af3c5f-9de2-4e24-bd72-f2891b981afd"
	lines := hookLines(t, "hooks-real-shell.jsonl")
	var other [][]byte
	for _, l := range lines {
		other = append(other, []byte(strings.ReplaceAll(string(l), first, "0b5d3a10-0000-4000-8000-0000000000aa")))
	}
	for i, c := range conversationAfter(t, append(lines, other...)) {
		if c != "" {
			t.Fatalf("after hook %d: conversation %q named; a nested run would start the chat over", i+1, c)
		}
	}
}
