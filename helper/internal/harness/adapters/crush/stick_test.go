package crush

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func rec(t *testing.T, v any) harness.Record {
	t.Helper()
	b, _ := json.Marshal(v)
	return harness.Record{At: time.Unix(1, 0), Data: b}
}

func srow(id string, updated int) map[string]any {
	return map[string]any{"id": id, "title": id, "created_at": updated, "updated_at": updated}
}

func userMsg(session string) map[string]any {
	return map[string]any{"id": "m-" + session, "session_id": session, "role": "user",
		"parts": `[{"type":"text","data":{"text":"hi"}}]`, "created_at": 1, "updated_at": 1}
}

// Crush's database names no process: once the chat has content, a newer
// session from another Crush in the project does not take it over.
func TestCrushKeepsSessionOnceItHasContent(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("switched to %q", got)
	}
}

// A relaunch Togo made with --session moves the chat to that session.
func TestCrushFollowsItsOwnResume(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 50)))
	m.Map(rec(t, srow("c", 200)))
	m.Relaunched([]string{"crush", "--session", "b"}, time.Now())
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q, want b", got)
	}
}
