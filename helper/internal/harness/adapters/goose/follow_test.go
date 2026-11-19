package goose

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// soloProcs is a process view: others says whether another Goose runs in
// the folder outside the session's tree.
type soloProcs struct{ others bool }

func (soloProcs) Tree(context.Context) []int { return []int{10} }
func (soloProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (p soloProcs) Others(context.Context, []string, string) (bool, error) { return p.others, nil }

func userRow(id int, session string) map[string]any {
	return map[string]any{"id": id, "message_id": "m" + session, "session_id": session, "role": "user",
		"content_json": `[{"type":"text","text":"hi"}]`, "created_timestamp": 1}
}

// A new session or a resume in Goose's own UI writes another row after
// the followed one. With no other Goose in the folder the chat moves
// there, and stays before the new session's messages come; resuming the
// first moves it back.
func TestGooseFollowsAConversationItStartsItself(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj", Agent: "goose", Procs: soloProcs{}})
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q after the new one, want b", got)
	}
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q before b has messages, want b", got)
	}
	m.Map(row(t, userRow(2, "b")))
	m.Map(row(t, sess("a", "2026-11-12 10:09:00"))) // the resume
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("session %q after the resume, want a", got)
	}
}

// With another Goose in the folder a newer row may be that one's: the
// chat keeps its session (two sessions never share a guess).
func TestGooseKeepsItsSessionWithAnotherGooseInTheFolder(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj", Agent: "goose", Procs: soloProcs{others: true}})
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("switched to %q", got)
	}
}
