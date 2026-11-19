package pisession

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Pi's session file starts with a header naming the session. The mapper
// names that session as the conversation, so a /new or a resume (another
// header) makes the engine start the chat over on it; the same file read
// again (a replay after a helper restart) names the same one.
func TestSessionHeaderNamesTheConversation(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	const first = "7c1e2a90-3b4d-4c55-9e10-aa11bb22cc33"
	feed := func(s *Session, data []byte) {
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				s.Map(harness.Record{At: time.Now(), Data: line})
			}
		}
	}
	s := New("pi.session", harness.Env{})
	feed(s, b)
	if got := s.State()[harness.StateConversation]; got != first {
		t.Fatalf("conversation %q, want the header's session", got)
	}
	feed(s, b)
	if got := s.State()[harness.StateConversation]; got != first {
		t.Fatalf("conversation %q after the same file again", got)
	}
	other := strings.ReplaceAll(string(b), first, "0d9b5c11-aaaa-4bbb-8ccc-000000000002")
	feed(s, []byte(other))
	if got := s.State()[harness.StateConversation]; got != "0d9b5c11-aaaa-4bbb-8ccc-000000000002" {
		t.Fatalf("conversation %q after another session's file", got)
	}
	if n := s.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q: the named conversation is the engine's signal", n)
	}
}
