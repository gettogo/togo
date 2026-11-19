package crush

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

type soloProcs struct {
	mu     sync.Mutex
	others bool
}

func (*soloProcs) Tree(context.Context) []int { return []int{10} }
func (*soloProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (p *soloProcs) Others(context.Context, []string, string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.others, nil
}
func (p *soloProcs) set(others bool) { p.mu.Lock(); p.others = others; p.mu.Unlock() }

func msgOf(id, session string) map[string]any {
	m := userMsg(session)
	m["id"] = id
	return m
}

func crushEnv(t *testing.T, procs harness.Procs) harness.Env {
	return harness.Env{SessionID: "s_crushfollow", Cwd: "/w/proj", Agent: "crush", Procs: procs,
		Vars: map[string]string{source.VarTogoHome: filepath.Join(t.TempDir(), ".togo")}}
}

// settles reads the state, as the engine does when the lookup wakes it,
// until the followed session is want or a second passes.
func settles(m *db, want string) string {
	deadline := time.Now().Add(time.Second)
	for {
		got := m.State()["harness_session"]
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ctrl+n or a resume in Crush's own UI writes another session after the
// followed one. With no other Crush in the project the chat moves there,
// and stays before the new session's messages come; resuming the first
// moves it back.
func TestCrushFollowsAConversationItStartsItself(t *testing.T) {
	m := newDB(crushEnv(t, &soloProcs{}))
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("session %q after the new one, want b", got)
	}
	if n := m.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after a move, want 1: the engine starts the chat over on it", n)
	}
	m.Map(rec(t, srow("b", 200)))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q before b has messages, want b", got)
	}
	m.Map(rec(t, msgOf("m-b1", "b")))
	m.Map(rec(t, map[string]any{"id": "a", "title": "a", "created_at": 100, "updated_at": 300})) // the resume
	if got := settles(m, "a"); got != "a" {
		t.Fatalf("session %q after the resume, want a", got)
	}
}

// With another Crush in the project a newer session may be that one's:
// the chat keeps its session.
func TestCrushKeepsItsSessionWithAnotherCrushInTheProject(t *testing.T) {
	m := newDB(crushEnv(t, &soloProcs{others: true}))
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := settles(m, "b"); got != "a" {
		t.Fatalf("switched to %q", got)
	}
}

// The review's sequence: the pane moves at t=0 while alone; at t=3 a
// second Crush starts in the project and writes its session. The answer
// from t=0 does not carry the chat there.
func TestCrushDoesNotFollowASecondCrushAfterAMove(t *testing.T) {
	procs := &soloProcs{}
	m := newDB(crushEnv(t, procs))
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("t=0: session %q, want b", got)
	}
	m.Map(rec(t, msgOf("m-b1", "b")))
	procs.set(true)
	m.Map(rec(t, srow("c", 230)))
	if got := settles(m, "c"); got != "b" {
		t.Fatalf("followed the second Crush's session: %q", got)
	}
}

// A move, and a resume of an older session, are remembered across a
// helper restart.
func TestCrushRestartKeepsTheMove(t *testing.T) {
	env := crushEnv(t, &soloProcs{})
	m := newDB(env)
	m.Map(rec(t, srow("a", 100)))
	m.Map(rec(t, userMsg("a")))
	m.Map(rec(t, srow("b", 200)))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("session %q, want b", got)
	}
	restarted := newDB(env)
	restarted.Map(rec(t, srow("a", 100)))
	restarted.Map(rec(t, srow("b", 200)))
	if got := restarted.State()["harness_session"]; got != "b" {
		t.Fatalf("after a restart the chat is on %q, want b", got)
	}
	// The helper restart does not start the chat over.
	if n := restarted.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q after a helper restart, want none", n)
	}
	restarted.Map(rec(t, map[string]any{"id": "old", "title": "old", "created_at": 10, "updated_at": 300}))
	if got := settles(restarted, "old"); got != "old" {
		t.Fatalf("resume: session %q, want old", got)
	}
	again := newDB(env)
	again.Map(rec(t, srow("b", 200)))
	again.Map(rec(t, map[string]any{"id": "old", "title": "old", "created_at": 10, "updated_at": 300}))
	if got := again.State()["harness_session"]; got != "old" {
		t.Fatalf("after a second restart the chat is on %q, want old", got)
	}
}
