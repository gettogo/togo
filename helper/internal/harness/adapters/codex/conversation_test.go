package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The main thread names the conversation. A recorded session read twice
// (a helper restart) names the same one throughout; a turn on another
// top-level thread (/new or /resume at the desk) moves it; the older
// thread's resume result read again does not take it back.
func TestMainThreadNamesTheConversation(t *testing.T) {
	recs := records(t, "appserver-turn.jsonl")
	m := NewAppServer(harness.Env{SessionID: "s_conv"})
	var seen []string
	for range 2 {
		for _, r := range recs {
			m.Map(r)
			if c := m.State()[harness.StateConversation]; c != "" && (len(seen) == 0 || seen[len(seen)-1] != c) {
				seen = append(seen, c)
			}
		}
	}
	const main = "019a0000-0000-7000-8000-000000000001"
	if len(seen) != 1 || seen[0] != main {
		t.Fatalf("conversations named %q, want only %q", seen, main)
	}
	const other = "019a0000-0000-7000-8000-0000000000ff"
	started := `{"method": "turn/started", "params": {"threadId": "` + other + `", "turn": {"id": "019a0000-0000-7000-8000-0000000000f1", "items": [], "status": "inProgress", "startedAt": 1794009000}}}`
	m.Map(harness.Record{At: time.Unix(1794009000, 0), Data: json.RawMessage(started)})
	if c := m.State()[harness.StateConversation]; c != other {
		t.Fatalf("conversation %q after a turn on a new top-level thread, want %q", c, other)
	}
	for _, r := range recs {
		if strings.Contains(string(r.Data), `"result"`) && strings.Contains(string(r.Data), main) {
			m.Map(r)
			break
		}
	}
	if c := m.State()[harness.StateConversation]; c != other {
		t.Fatalf("the older thread read again took the chat back: %q", c)
	}
}
