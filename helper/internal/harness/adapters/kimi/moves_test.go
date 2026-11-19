package kimi

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Kimi Code writes one wire log per conversation. The mapper names the log
// as the conversation, so a /new or a resume (another log) makes the
// engine start the chat over on it; the same log read again (a replay
// after a helper restart) names the same one.
func TestWireLogNamesTheConversation(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "wire-h1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWire(harness.Env{})
	feed := func(file string) {
		for _, line := range bytes.Split(b, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				w.Map(harness.Record{At: time.Now(), File: file, Data: line})
			}
		}
	}
	feed("/k/a/wire.jsonl")
	feed("/k/a/wire.jsonl")
	if got := w.State()[harness.StateConversation]; got != "/k/a/wire.jsonl" {
		t.Fatalf("conversation %q on one log read twice", got)
	}
	feed("/k/b/wire.jsonl")
	if got := w.State()[harness.StateConversation]; got != "/k/b/wire.jsonl" {
		t.Fatalf("conversation %q after another log", got)
	}
	if n := w.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q: the named conversation is the engine's signal", n)
	}
}
