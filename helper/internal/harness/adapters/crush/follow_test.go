package crush

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

type soloProcs struct{ others bool }

func (soloProcs) Tree(context.Context) []int { return []int{10} }
func (soloProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (p soloProcs) Others(context.Context, []string, string) (bool, error) { return p.others, nil }

func msgOf(id, session string) map[string]any {
	m := userMsg(session)
	m["id"] = id
	return m
}

// ctrl+n or a resume in Crush's own UI writes another session after the
// followed one. With no other Crush in the project the chat moves there,
// and stays before the new session's messages come; resuming the first
// moves it back.
func TestCrushFollowsAConversationItStartsItself(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj", Agent: "crush", Procs: soloProcs{}})
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q after the new one, want b", got)
	}
	m.Map(rec(t, srow("b", 200)))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q before b has messages, want b", got)
	}
	m.Map(rec(t, msgOf("m-b1", "b")))
	m.Map(rec(t, map[string]any{"id": "a", "title": "a", "created_at": 100, "updated_at": 300})) // the resume
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("session %q after the resume, want a", got)
	}
}

// With another Crush in the project a newer session may be that one's:
// the chat keeps its session.
func TestCrushKeepsItsSessionWithAnotherCrushInTheProject(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj", Agent: "crush", Procs: soloProcs{others: true}})
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := m.State()["harness_session"]; got != "a" {
		t.Fatalf("switched to %q", got)
	}
}
