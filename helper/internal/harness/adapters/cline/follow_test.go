package cline

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// interactive rewrites a session's manifest as the TUI writes it.
func interactive(t *testing.T, h, id string) {
	t.Helper()
	p := filepath.Join(h, ".cline", "data", "sessions", id, id+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), `"interactive": false`, `"interactive": true`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// follows maps doc until the lookup it waits on has ended (the engine
// reads State when woken) and reports the session followed.
func follows(m *messages, doc []byte, want string) string {
	deadline := time.Now().Add(time.Second)
	for {
		m.Map(harness.Record{Data: doc})
		got := m.State()["harness_session"]
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func clineEnv(h string, procs fakeProcs) harness.Env {
	e := env(h, "2026-11-08T03:37:52Z")
	e.Agent = "cline"
	e.Procs = procs
	return e
}

// The recorded manifests carry no pid. A /new in the TUI writes a newer,
// interactive session of the folder: with no other Cline there it is
// this pane's, and the chat moves to it; a resume of the first (its
// document rewritten later) moves it back.
func TestMessagesFollowsANewSessionWithoutPidWhenAlone(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	other := addSession(t, h, "1794200000003_nopid", 0, doc)
	interactive(t, h, "1794200000003_nopid")
	m := newMessages(clineEnv(h, fakeProcs{tree: []int{100}}))
	if items, _ := m.Map(harness.Record{Data: doc}); len(items) == 0 {
		t.Fatal("first session not followed")
	}
	m.Map(harness.Record{Data: doc}) // read again: a replay is not a move
	if n := m.State()[harness.StateMoves]; n != "" {
		t.Fatalf("moves %q on one session read twice", n)
	}
	if got := follows(m, other, "1794200000003_nopid"); got != "1794200000003_nopid" {
		t.Fatalf("the new session is not followed: %q", got)
	}
	if n := m.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after a move, want 1: the engine starts the chat over on it", n)
	}
	resumed := []byte(strings.Replace(string(doc), "2026-11-08T03:38:03.002Z", "2026-11-08T05:02:00.000Z", 1))
	if got := follows(m, resumed, fixtureID); got != fixtureID {
		t.Fatalf("the resume is not followed: %q", got)
	}
}

// A one-shot `cline <task>` in the folder writes interactive false: it is
// never this pane's TUI, even with no other Cline left running.
func TestMessagesDoesNotFollowAOneShotRun(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	oneShot := addSession(t, h, "1794200000004_oneshot", 0, doc)
	m := newMessages(clineEnv(h, fakeProcs{tree: []int{100}}))
	m.Map(harness.Record{Data: doc})
	if got := follows(m, oneShot, "1794200000004_oneshot"); got != fixtureID {
		t.Fatalf("followed a one-shot run: %q", got)
	}
}

// The review's sequence: the pane moves at t=0 while alone; at t=3 a
// second Cline starts in the folder and writes its session. The answer
// from t=0 does not carry the chat there.
func TestMessagesDoesNotFollowASecondClineAfterAMove(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	second := addSession(t, h, "1794200000005_mine", 0, doc)
	interactive(t, h, "1794200000005_mine")
	flip := &atomic.Bool{}
	m := newMessages(clineEnv(h, fakeProcs{tree: []int{100}, flip: flip}))
	m.Map(harness.Record{Data: doc})
	if got := follows(m, second, "1794200000005_mine"); got != "1794200000005_mine" {
		t.Fatalf("t=0: %q", got)
	}
	flip.Store(true)
	third := addSession(t, h, "1794200000006_other", 0, doc)
	interactive(t, h, "1794200000006_other")
	third = []byte(strings.Replace(string(third), "2026-11-08T05:01:00.000Z", "2026-11-08T05:03:00.000Z", 1))
	if got := follows(m, third, "1794200000006_other"); got != "1794200000005_mine" {
		t.Fatalf("followed the second Cline's session: %q", got)
	}
}

// The document that waited on the lookup is mapped when the lookup ends
// (Woken), not at the next rewrite: Cline may write nothing more for a
// while.
func TestMessagesMapsTheWaitingDocumentWhenTheLookupEnds(t *testing.T) {
	h, doc := home(t)
	interactive(t, h, fixtureID)
	other := addSession(t, h, "1794200000007_nopid", 0, doc)
	interactive(t, h, "1794200000007_nopid")
	m := newMessages(clineEnv(h, fakeProcs{tree: []int{100}}))
	woke := make(chan struct{}, 1)
	m.Wake(func() { woke <- struct{}{} })
	m.Map(harness.Record{Data: doc})
	if items, _ := m.Map(harness.Record{Data: other}); len(items) != 0 {
		t.Fatal("moved before the lookup ended")
	}
	select {
	case <-woke:
	case <-time.After(3 * time.Second):
		t.Fatal("the engine was not woken")
	}
	items, _ := m.Woken()
	if len(items) == 0 || m.State()["harness_session"] != "1794200000007_nopid" {
		t.Fatalf("Woken drew %d items, session %q", len(items), m.State()["harness_session"])
	}
}
