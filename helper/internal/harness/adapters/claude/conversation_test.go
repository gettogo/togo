package claude

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// /clear and /resume move Claude Code to another transcript file. The
// transcript mapper names the file's session as the conversation, so the
// engine starts the chat over on it; the same file read again (a helper
// restart) and a subagent's file change nothing.
func TestTranscriptFileNamesTheConversation(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	m := newTranscript(harness.Env{Cwd: "/HOME/proj"})
	feed := func(file string, b []byte) {
		for _, line := range bytes.Split(b, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				m.Map(harness.Record{At: time.Now(), File: file, Data: line})
			}
		}
	}
	dir := filepath.Join(t.TempDir(), "projects", "-HOME-proj")
	main := filepath.Join(dir, "ca4d237e-2b33-48c7-914c-8a8a6f490d08.jsonl")
	feed(main, read("transcript-real.jsonl"))
	feed(main, read("transcript-real.jsonl"))
	feed(filepath.Join(dir, "ca4d237e-2b33-48c7-914c-8a8a6f490d08", "subagents", "agent-a1.jsonl"), read("subagent-real.jsonl"))
	if got := m.State()[harness.StateConversation]; got != "ca4d237e-2b33-48c7-914c-8a8a6f490d08" {
		t.Fatalf("conversation %q, want the transcript's session", got)
	}
	feed(filepath.Join(dir, "5b0e9c7a-1111-4222-8333-444455556666.jsonl"), read("transcript-continued.jsonl"))
	if got := m.State()[harness.StateConversation]; got != "5b0e9c7a-1111-4222-8333-444455556666" {
		t.Fatalf("conversation %q after /clear moved the transcript", got)
	}
}
