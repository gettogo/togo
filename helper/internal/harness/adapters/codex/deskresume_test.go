package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// lateOf is a resume record as the feed marks it when the server loaded
// the thread after the feed first asked: the desk resumed it.
func lateOf(t *testing.T, rec harness.Record) harness.Record {
	t.Helper()
	var m struct {
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(rec.Data, &m); err != nil {
		t.Fatal(err)
	}
	m.Params["late"] = json.RawMessage("true")
	b, _ := json.Marshal(m)
	// Read when the desk resumed it: after every turn in the fixtures.
	return harness.Record{At: time.Unix(1794000200, 0), Data: b}
}

// startOver maps rec as the engine does: when the mapper names another
// conversation, a fresh mapper, given what the old one carries, maps the
// same record again and its items are the chat's.
func startOver(t *testing.T, m *AppServer, rec harness.Record) (*AppServer, []harness.Item) {
	t.Helper()
	before := m.State()[harness.StateConversation]
	items, _ := m.Map(rec)
	if now := m.State()[harness.StateConversation]; now == before {
		return m, items
	}
	fresh := NewAppServer(m.env)
	m.Carry(fresh)
	items, _ = fresh.Map(rec)
	return fresh, items
}

func turnStartedOn(thread, turn string, at int64) harness.Record {
	raw := `{"method": "turn/started", "params": {"threadId": "` + thread + `", "turn": {"id": "` + turn + `", "items": [], "status": "inProgress", "startedAt": ` + itoa(int(at)) + `}}}`
	return harness.Record{At: t0, Data: json.RawMessage(raw)}
}

// Seen live: after /resume of an earlier conversation at the desk the chat
// did not change until the next prompt. The server loads the resumed
// conversation and the feed joins it at once, marked late; it is older
// than the one shown, so the freshness rule kept it out. Now a late join
// moves the chat there with the conversation's earlier turns.
func TestDeskResumeOfAnUnloadedConversationMovesAtOnce(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_codexdesk1"})
	m.Map(resumeOf(t, threadB, 1794000100)) // the conversation shown
	m, items := startOver(t, m, lateOf(t, resumeOf(t, threadA, 1794000000)))
	if got := m.State()["harness_session"]; got != threadA {
		t.Fatalf("main thread %q after the desk resumed %q", got, threadA)
	}
	ids := itemIDs(items)
	for _, want := range []string{"u-old", "msg_old"} {
		if !strings.Contains(ids, want) {
			t.Fatalf("the resumed conversation's history is missing %s: %s", want, ids)
		}
	}
	if strings.Contains(ids, "-b") {
		t.Fatalf("the left conversation's items came along: %s", ids)
	}

	// The connection drops and the feed joins both again: nothing is late
	// then, and the conversation left, though its last turn is newer than
	// the resumed one's, does not take the chat back.
	items, _ = m.Map(resumeOf(t, threadB, 1794000100))
	if got := m.State()["harness_session"]; got != threadA || len(items) != 0 {
		t.Fatalf("after a reconnect: main %q, items %s", got, itemIDs(items))
	}
}

// A late join of the thread already shown, or of an ephemeral one (Codex's
// title thread), moves nothing.
func TestLateJoinThatIsNotAMove(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_codexdesk2"})
	m.Map(resumeOf(t, threadA, 1794000100))
	if _, items := startOver(t, m, lateOf(t, resumeOf(t, threadA, 1794000100))); m.State()["harness_session"] != threadA || len(items) == 0 {
		t.Fatalf("the same thread again: main %q, items %d", m.State()["harness_session"], len(items))
	}
	eph := lateOf(t, resumeOf(t, threadB, 1794000000)) // older: only a late join could move to it
	eph.Data = json.RawMessage(strings.Replace(string(eph.Data), `"ephemeral":false`, `"ephemeral":true`, 1))
	if !strings.Contains(string(eph.Data), `"ephemeral":true`) {
		t.Fatal("fixture: no ephemeral field")
	}
	if items, _ := m.Map(eph); m.State()["harness_session"] != threadA || len(items) != 0 {
		t.Fatalf("an ephemeral thread took the chat: main %q, items %s", m.State()["harness_session"], itemIDs(items))
	}
}

// Seen live: after /resume at the desk of a conversation the server still
// had loaded, the chat moved at the next prompt and showed only that new
// turn. Nothing reaches the feed before the turn starts; then the feed
// joins the thread again (rpc.threads), and the chat that started over on
// it shows its earlier turns as well as the new one.
func TestPromptMoveShowsTheResumedConversationsHistory(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_codexdesk3"})
	m.Map(resumeOf(t, threadA, 1794000000))
	// /new at the desk: a new thread, loaded late.
	m, _ = startOver(t, m, lateOf(t, resumeOf(t, threadB, 1794000100)))
	if got := m.State()["harness_session"]; got != threadB {
		t.Fatalf("after /new: main %q", got)
	}
	// /resume of A, still loaded: its next turn moves the chat.
	m, items := startOver(t, m, turnStartedOn(threadA, "t-resumed", 1794000300))
	if got := m.State()["harness_session"]; got != threadA {
		t.Fatalf("after the prompt: main %q", got)
	}
	if ids := itemIDs(items); strings.Contains(ids, "u-old") {
		t.Fatalf("history before the join: %s", ids)
	}
	// The feed's second join of A.
	m, items = startOver(t, m, resumeOf(t, threadA, 1794000000))
	if got := m.State()["harness_session"]; got != threadA {
		t.Fatalf("the join moved the chat: main %q", got)
	}
	ids := itemIDs(items)
	for _, want := range []string{"u-old", "msg_old"} {
		if !strings.Contains(ids, want) {
			t.Fatalf("the resumed conversation's history is missing %s: %s", want, ids)
		}
	}
	if strings.Contains(ids, "-b") {
		t.Fatalf("the left conversation's items came along: %s", ids)
	}
}
