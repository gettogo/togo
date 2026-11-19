package pisession

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A header of another session (a /new or a resume) raises StateMoves, so
// the engine ends what the conversation left had; the same header read
// again (a replay) does not.
func TestAnotherSessionHeaderIsAMove(t *testing.T) {
	s := New("pi.session", harness.Env{})
	head := func(id string) {
		s.Map(harness.Record{At: time.Now(), Data: []byte(`{"type":"session","id":"` + id + `","timestamp":"2026-11-12T10:00:00Z"}`)})
	}
	head("a")
	if n := s.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q on the first header", n)
	}
	head("a")
	head("b")
	if n := s.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after another session's header, want 1", n)
	}
}
