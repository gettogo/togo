package goose

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// soloProcs is a process view: others says whether another Goose runs in
// the folder outside the session's tree; it can change.
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

func userRow(id int, session string) map[string]any {
	return map[string]any{"id": id, "message_id": "m" + session, "session_id": session, "role": "user",
		"content_json": `[{"type":"text","text":"hi"}]`, "created_timestamp": 1}
}

func gooseEnv(t *testing.T, procs harness.Procs) harness.Env {
	return harness.Env{SessionID: "s_goosefollow", Cwd: "/w/proj", Agent: "goose", Procs: procs,
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

// A new session or a resume in Goose's own UI writes another row after
// the followed one. With no other Goose in the folder the chat moves
// there, and stays before the new session's messages come; resuming the
// first moves it back. Each move rests on a lookup begun after the row
// was seen, run off the session's lock.
func TestGooseFollowsAConversationItStartsItself(t *testing.T) {
	m := newDB(gooseEnv(t, &soloProcs{}))
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("session %q after the new one, want b", got)
	}
	if n := m.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after a move, want 1: the engine ends what the left conversation had", n)
	}
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := m.State()["harness_session"]; got != "b" {
		t.Fatalf("session %q before b has messages, want b", got)
	}
	m.Map(row(t, userRow(2, "b")))
	m.Map(row(t, sess("a", "2026-11-12 10:09:00"))) // the resume
	if got := settles(m, "a"); got != "a" {
		t.Fatalf("session %q after the resume, want a", got)
	}
}

// With another Goose in the folder a newer row may be that one's: the
// chat keeps its session (two sessions never share a guess).
func TestGooseKeepsItsSessionWithAnotherGooseInTheFolder(t *testing.T) {
	m := newDB(gooseEnv(t, &soloProcs{others: true}))
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := settles(m, "b"); got != "a" {
		t.Fatalf("switched to %q", got)
	}
}

// The review's sequence: the pane moves at t=0 while alone; at t=3 a
// second Goose starts in the folder and writes its row. The answer from
// t=0 does not carry the chat to the second Goose's conversation.
func TestGooseDoesNotFollowASecondGooseAfterAMove(t *testing.T) {
	procs := &soloProcs{}
	m := newDB(gooseEnv(t, procs))
	m.Map(row(t, sess("a", "2026-11-12 10:00:00")))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, sess("b", "2026-11-12 10:05:00")))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("t=0: session %q, want b", got)
	}
	m.Map(row(t, userRow(2, "b")))
	procs.set(true)                                 // t=3: another Goose starts here
	m.Map(row(t, sess("c", "2026-11-12 10:06:00"))) // and writes its conversation
	if got := settles(m, "c"); got != "b" {
		t.Fatalf("followed the second Goose's conversation: %q", got)
	}
}

// A move is remembered across a helper restart, and so is a resume of an
// older conversation, which the first pick would never choose.
func TestGooseRestartKeepsTheMove(t *testing.T) {
	env := gooseEnv(t, &soloProcs{})
	env.Vars["created"] = "2026-11-12T10:00:00Z"
	m := newDB(env)
	m.Map(row(t, map[string]string{"id": "a", "working_dir": "/w/proj", "created_at": "2026-11-12 10:00:01", "updated_at": "2026-11-12 10:01:00"}))
	m.Map(row(t, userRow(1, "a")))
	m.Map(row(t, map[string]string{"id": "b", "working_dir": "/w/proj", "created_at": "2026-11-12 10:04:00", "updated_at": "2026-11-12 10:05:00"}))
	if got := settles(m, "b"); got != "b" {
		t.Fatalf("session %q, want b", got)
	}
	restarted := newDB(env)
	restarted.Map(row(t, map[string]string{"id": "a", "working_dir": "/w/proj", "created_at": "2026-11-12 10:00:01", "updated_at": "2026-11-12 10:01:00"}))
	restarted.Map(row(t, map[string]string{"id": "b", "working_dir": "/w/proj", "created_at": "2026-11-12 10:04:00", "updated_at": "2026-11-12 10:05:00"}))
	if got := restarted.State()["harness_session"]; got != "b" {
		t.Fatalf("after a restart the chat is on %q, want b", got)
	}
	// Resume an older conversation, made before this session began.
	restarted.Map(row(t, map[string]string{"id": "old", "working_dir": "/w/proj", "created_at": "2026-11-11 09:00:00", "updated_at": "2026-11-12 10:09:00"}))
	if got := settles(restarted, "old"); got != "old" {
		t.Fatalf("resume: session %q, want old", got)
	}
	again := newDB(env)
	again.Map(row(t, map[string]string{"id": "b", "working_dir": "/w/proj", "created_at": "2026-11-12 10:04:00", "updated_at": "2026-11-12 10:05:00"}))
	again.Map(row(t, map[string]string{"id": "old", "working_dir": "/w/proj", "created_at": "2026-11-11 09:00:00", "updated_at": "2026-11-12 10:09:00"}))
	if got := again.State()["harness_session"]; got != "old" {
		t.Fatalf("after a second restart the chat is on %q, want old", got)
	}
}
