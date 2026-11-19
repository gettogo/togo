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

// chat stands in for the engine: when the mapper names another
// conversation, the chat starts over with a fresh mapper, which is handed
// what the old one carries and maps the announcing record again.
type chat struct {
	m     *AppServer
	conv  string
	overs int
}

func (c *chat) feed(r harness.Record) {
	c.m.Map(r)
	got := c.m.State()[harness.StateConversation]
	if got == "" || got == c.conv {
		return
	}
	if c.conv == "" {
		c.conv = got
		return
	}
	fresh := NewAppServer(c.m.env)
	if carrier, ok := any(c.m).(harness.Carrier); ok {
		carrier.Carry(fresh)
	}
	c.m, c.overs = fresh, c.overs+1
	c.m.Map(r)
	c.conv = c.m.State()[harness.StateConversation]
}

// Found by the end-to-end suite: /new at the desk, then /resume at the
// desk of the thread the helper had joined before /new. The feed joins
// each thread once per connection, so the earlier thread is not described
// again; its turn still moves the chat back to it, and the chat starts
// over on it. A subagent's turn after the start-over still moves nothing.
func TestDeskResumeOfAnEarlierThreadMovesTheChat(t *testing.T) {
	recs := records(t, "appserver-turn.jsonl")
	const main = "019a0000-0000-7000-8000-000000000001"
	const child = "019a0000-0000-7000-8000-000000000002"
	const other = "019a0000-0000-7000-8000-0000000000ff"
	c := &chat{m: NewAppServer(harness.Env{SessionID: "s_conv_resume"})}
	for _, r := range recs {
		c.feed(r)
	}
	if c.conv != main || c.overs != 0 {
		t.Fatalf("after the recorded session: conversation %q, %d start-overs", c.conv, c.overs)
	}
	at := time.Unix(1794009000, 0)
	turn := func(thread, id string) harness.Record {
		return harness.Record{At: at, Data: json.RawMessage(`{"method": "turn/started", "params": {"threadId": "` + thread +
			`", "turn": {"id": "` + id + `", "items": [], "status": "inProgress", "startedAt": 1794009000}}}`)}
	}
	// /new: the server lists the new top-level thread, then its turn starts.
	listed := strings.Replace(strings.Replace(string(recs[0].Data), main, other, 1), `"status": {"type": "idle"}`, `"status": {"type": "active"}`, 1)
	c.feed(harness.Record{At: at, Data: json.RawMessage(listed)})
	c.feed(turn(other, "019a0000-0000-7000-8000-0000000000f1"))
	if c.conv != other || c.overs != 1 {
		t.Fatalf("after /new: conversation %q, %d start-overs", c.conv, c.overs)
	}
	// /resume of the first thread: no resume result, only its next turn.
	c.feed(turn(main, "019a0000-0000-7000-8000-0000000000f2"))
	if c.conv != main || c.overs != 2 {
		t.Fatalf("after /resume of the earlier thread: conversation %q, %d start-overs, want %q after 2", c.conv, c.overs, main)
	}
	if got := c.m.State()["harness_session"]; got != main {
		t.Fatalf("helper calls would name %q, want %q", got, main)
	}
	// The subagent of the first thread was described to the mapper left
	// behind, not to this one: its turn is not taken as the main thread's.
	c.feed(turn(child, "019a0000-0000-7000-8000-0000000000f3"))
	if c.conv != main || c.overs != 2 {
		t.Fatalf("a subagent's turn moved the chat: conversation %q, %d start-overs", c.conv, c.overs)
	}
}
