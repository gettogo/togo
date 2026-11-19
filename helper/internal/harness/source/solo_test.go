package source

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// countingProcs counts lookups and can block them.
type countingProcs struct {
	*fakeProcs
	n     atomic.Int32
	block chan struct{}
}

func (c *countingProcs) Others(ctx context.Context, cmds []string, cwd string) (bool, error) {
	c.n.Add(1)
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return c.fakeProcs.Others(ctx, cmds, cwd)
}

// soloAsk calls Alone until a lookup begun at or after since has answered.
func soloAsk(t *testing.T, s *Solo, env harness.Env, since time.Time) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.Alone(env, since, nil) {
			return true
		}
		s.mu.Lock()
		answered := s.done && !s.asking && !s.began.Before(since)
		s.mu.Unlock()
		if answered {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the lookup never ended")
	return false
}

// Alone is yes only on evidence: a process view that lists no other agent
// of the kind in the folder. No view, a failed lookup or another agent
// there is no.
func TestSoloAlone(t *testing.T) {
	procs := &fakeProcs{tree: []int{100}}
	env := harness.Env{Agent: "goose", Cwd: "/w/proj", Procs: procs}
	if !soloAsk(t, &Solo{}, env, time.Now()) {
		t.Fatal("alone in the folder: no")
	}
	procs.others = map[string]bool{"/w/proj": true}
	if soloAsk(t, &Solo{}, env, time.Now()) {
		t.Fatal("another agent in the folder: yes")
	}
	procs.others, procs.err = nil, context.DeadlineExceeded
	if soloAsk(t, &Solo{}, env, time.Now()) {
		t.Fatal("failed lookup: yes")
	}
	if soloAsk(t, &Solo{}, harness.Env{Agent: "goose", Cwd: "/w/proj"}, time.Now()) {
		t.Fatal("no process view: yes")
	}
}

// The finding behind the rule: pane A's agent moves at t=0 while alone;
// at t=3 a second agent of the kind starts in the folder and writes a
// conversation. An "alone" answer from before that conversation was seen
// does not move A there: the move waits for a lookup begun after it,
// which sees the second agent.
func TestSoloAloneIsNeverReusedForALaterConversation(t *testing.T) {
	procs := &fakeProcs{tree: []int{100}}
	env := harness.Env{Agent: "goose", Cwd: "/w/proj", Procs: procs}
	s := &Solo{}
	if !soloAsk(t, s, env, time.Now()) {
		t.Fatal("t=0: alone, no")
	}
	procs.mu.Lock()
	procs.others = map[string]bool{"/w/proj": true} // t=3: the second agent starts
	procs.mu.Unlock()
	seen := time.Now() // ... and its conversation is seen
	if s.Alone(env, seen, nil) {
		t.Fatal("the earlier alone answer moved the session to a later conversation")
	}
	if soloAsk(t, s, env, seen) {
		t.Fatal("a fresh lookup missed the second agent")
	}
}

// A "not alone" answer is reused for a while; lookups never run on the
// caller's goroutine, so a blocked one does not hold it up.
func TestSoloNeverBlocksAndReusesNotAlone(t *testing.T) {
	procs := &countingProcs{fakeProcs: &fakeProcs{tree: []int{100}, others: map[string]bool{"/w/proj": true}}, block: make(chan struct{})}
	env := harness.Env{Agent: "goose", Cwd: "/w/proj", Procs: procs}
	s := &Solo{}
	start := time.Now()
	woke := make(chan struct{}, 1)
	for i := 0; i < 5; i++ {
		if s.Alone(env, start, func() { woke <- struct{}{} }) {
			t.Fatal("alone while the lookup is blocked")
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("Alone blocked for %v", d)
	}
	close(procs.block)
	select {
	case <-woke:
	case <-time.After(3 * time.Second):
		t.Fatal("the lookup never ended")
	}
	if s.Alone(env, start, nil) {
		t.Fatal("not alone: yes")
	}
	before := procs.n.Load()
	for i := 0; i < 5; i++ {
		s.Alone(env, time.Now(), nil)
	}
	if procs.n.Load() != before {
		t.Fatalf("a not-alone answer was not reused: %d lookups", procs.n.Load()-before)
	}
}

// Also names other programs that write the same store.
func TestSoloAsksAboutSiblingPrograms(t *testing.T) {
	var got []string
	procs := &cmdProcs{fakeProcs: &fakeProcs{tree: []int{100}}, seen: &got}
	env := harness.Env{Agent: "vibe", Cwd: "/w/proj", Procs: procs}
	(&Solo{Also: []string{"vibe-acp", "vibe-app-server"}}).Check(env)
	if len(got) != 3 || got[0] != "vibe" || got[1] != "vibe-acp" || got[2] != "vibe-app-server" {
		t.Fatalf("asked about %v", got)
	}
}

type cmdProcs struct {
	*fakeProcs
	seen *[]string
}

func (c *cmdProcs) Others(ctx context.Context, cmds []string, cwd string) (bool, error) {
	*c.seen = cmds
	return c.fakeProcs.Others(ctx, cmds, cwd)
}

// What a mapper follows is remembered per name, 0600, and forgotten with
// the session.
func TestFollowedIsRememberedAndRemoved(t *testing.T) {
	home := t.TempDir()
	env := harness.Env{SessionID: "s_followed01", Vars: map[string]string{VarTogoHome: home}}
	SaveFollowed(env, "goose", "20261112_7")
	if got := LoadFollowed(env, "goose"); got != "20261112_7" {
		t.Fatalf("loaded %q", got)
	}
	fi, err := os.Stat(filepath.Join(home, "run", "follow-s_followed01.goose.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %v %v", fi, err)
	}
	saveFollow(env, "/x.jsonl")
	RemoveFollows(home, "s_followed01")
	if LoadFollowed(env, "goose") != "" || loadFollow(env) != "" {
		t.Fatal("a removed session's follows are kept")
	}
	SaveFollowed(env, "goose", "a")
	SaveFollowed(env, "goose", "")
	if LoadFollowed(env, "goose") != "" {
		t.Fatal("an empty value did not forget")
	}
}
