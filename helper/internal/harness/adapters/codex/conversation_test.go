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
	// /new at the desk: the server lists the new top-level thread (no
	// parent, working), then its turn starts.
	const other = "019a0000-0000-7000-8000-0000000000ff"
	listed := strings.Replace(strings.Replace(string(recs[0].Data), main, other, 1), `"status": {"type": "idle"}`, `"status": {"type": "active"}`, 1)
	m.Map(harness.Record{At: time.Unix(1794009000, 0), Data: json.RawMessage(listed)})
	started := `{"method": "turn/started", "params": {"threadId": "` + other + `", "turn": {"id": "019a0000-0000-7000-8000-0000000000f1", "items": [], "status": "inProgress", "startedAt": 1794009000}}}`
	m.Map(harness.Record{At: time.Unix(1794009000, 0), Data: json.RawMessage(started)})
	if c := m.State()[harness.StateConversation]; c != other {
		t.Fatalf("conversation %q after /new, want %q", c, other)
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

// Seen in review: a subagent's turn/started can arrive before its spawn
// completes and before the server lists the child thread. The recorded
// session replayed in that order keeps the parent as the conversation
// throughout, and the child's turn is drawn under its agent once known.
func TestSubagentTurnBeforeItsSpawnKeepsTheConversation(t *testing.T) {
	recs := records(t, "appserver-turn.jsonl")
	// recs[26] completes the spawn, recs[27] lists the child, recs[28]
	// starts the child's turn: move the turn ahead of both.
	if !strings.Contains(string(recs[26].Data), "call_spawn") || !strings.Contains(string(recs[28].Data), `"turn/started"`) {
		t.Fatal("the fixture changed: records 26 and 28 are not the spawn and the child's turn")
	}
	order := append(append(append([]harness.Record{}, recs[:26]...), recs[28], recs[26], recs[27]), recs[29:]...)
	m := NewAppServer(harness.Env{SessionID: "s_conv_child"})
	const main = "019a0000-0000-7000-8000-000000000001"
	for i, r := range order {
		m.Map(r)
		if c := m.State()[harness.StateConversation]; c != main {
			t.Fatalf("after record %d: conversation %q, want the parent %q", i, c, main)
		}
	}
}
