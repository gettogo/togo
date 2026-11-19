package opencode

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The engine reads a conversation's past through a fresh mapper when the
// chat moves to it (engine/history.go hydrate). After the phone resumed
// one conversation, OpenCode runs with `--session <that one>`, and every
// fresh mapper starts on it. When the terminal then moved to another
// conversation, its past was dropped as not followed, and the mapper's
// state still named the resumed one, so the engine took the replay for a
// move back and emptied the chat a second time. The past being read is
// the conversation the chat moved to: the mapper follows it.
func TestPastMessagesFollowTheirConversationOverTheLaunchFlag(t *testing.T) {
	m := NewEvents2(harness.Env{Cwd: "/w", Argv: []string{"opencode", "--session", "ses_first"}})
	if got := m.State()["conversation"]; got != "ses_first" {
		t.Fatalf("a fresh mapper follows %q, want the launch flag's ses_first", got)
	}
	msg := json.RawMessage(`{"id":"msg_1","type":"user","text":"In the second","time":{"created":1795000000000}}`)
	rec, _ := json.Marshal(map[string]any{"type": historyMessage, "conversation": "ses_second", "data": msg})
	items, _ := m.Map(harness.Record{Source: "events", At: time.Now(), Data: rec})
	if len(items) != 1 || items[0].User == nil || items[0].User.Text != "In the second" {
		t.Fatalf("the past of the conversation moved to drew %+v, want its prompt", items)
	}
	if got := m.State()["conversation"]; got != "ses_second" {
		t.Fatalf("after its past the mapper follows %q, want ses_second", got)
	}
	// A later past message of the same conversation keeps to it.
	msg = json.RawMessage(`{"id":"msg_2","type":"user","text":"Again","time":{"created":1795000001000}}`)
	rec, _ = json.Marshal(map[string]any{"type": historyMessage, "conversation": "ses_second", "data": msg})
	if items, _ := m.Map(harness.Record{Source: "events", At: time.Now(), Data: rec}); len(items) != 1 {
		t.Fatalf("a second past message drew %d items", len(items))
	}
}
