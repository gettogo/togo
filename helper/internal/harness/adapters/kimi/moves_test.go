package kimi

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Records from another log (a /new or a resume) raise StateMoves, so the
// engine ends what the conversation left had; the same log read again (a
// replay) does not.
func TestRecordsOfAnotherLogAreAMove(t *testing.T) {
	w := NewWire(harness.Env{})
	rec := func(file string) {
		w.Map(harness.Record{At: time.Now(), File: file, Data: []byte(`{"type":"metadata","protocol_version":"1.5"}`)})
	}
	rec("/k/a/wire.jsonl")
	rec("/k/a/wire.jsonl")
	if n := w.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q on one log", n)
	}
	rec("/k/b/wire.jsonl")
	if n := w.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after another log, want 1", n)
	}
}
