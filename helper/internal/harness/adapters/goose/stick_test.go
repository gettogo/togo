package goose

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func row(t *testing.T, v any) harness.Record {
	t.Helper()
	b, _ := json.Marshal(v)
	return harness.Record{At: time.Unix(1, 0), Data: b}
}

func sess(id, updated string) map[string]string {
	return map[string]string{"id": id, "working_dir": "/w/proj", "goose_mode": "auto", "updated_at": updated}
}

// Goose's database names no process, so a session another Goose creates in
// the same folder cannot be told from this one's. Once the chat has
// content, a newer row does not take it over.
func TestGooseKeepsSessionOnceItHasContent(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, map[string]any{"id": 1, "message_id": "m1", "session_id": "a", "role": "user",
		"content_json": `[{"type":"text","text":"hi"}]`, "created_timestamp": 1}))
	m.Map(row(t, sess("b", "2026-11-12 11:00:00")))
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("switched to %q", got)
	}
}

// A relaunch Togo made with --session-id moves the chat to that session.
func TestGooseFollowsItsOwnResume(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, map[string]any{"id": 1, "message_id": "m1", "session_id": "a", "role": "user",
		"content_json": `[{"type":"text","text":"hi"}]`, "created_timestamp": 1}))
	m.Map(row(t, sess("b", "2026-11-12 09:00:00")))
	m.Map(row(t, sess("c", "2026-11-12 11:00:00"))) // someone else's, newer
	m.Relaunched([]string{"goose", "session", "--resume", "--session-id", "b"}, time.Now())
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q, want b", got)
	}
}
