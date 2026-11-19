package vibe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// vibeSession writes a top-level session folder working in /w/proj with
// one journal segment last written age ago, as Vibe 3.1 lays them out.
func vibeSession(t *testing.T, home, id string, age time.Duration) (glob, seg string) {
	t.Helper()
	p := filepath.Join(home, ".vibe", "logs", "session", "unified", id)
	if err := os.MkdirAll(filepath.Join(p, "journal"), 0o700); err != nil {
		t.Fatal(err)
	}
	meta := `{"session_id":"` + id + `","parent_session_id":null,"environment":{"working_directory":"/w/proj"}}`
	if err := os.WriteFile(filepath.Join(p, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	seg = filepath.Join(p, "journal", "0000000000000001.jsonl")
	if err := os.WriteFile(seg, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	_ = os.Chtimes(seg, at, at)
	return filepath.Join(p, "journal", "*.jsonl"), seg
}

// Vibe started again without --continue writes a new session folder; a
// resume writes a new segment of an earlier one. The session follows the
// journal written last when no other Vibe runs in the folder, and keeps
// its journal when one does or when the newer journal is another
// process's.
func TestJournalForFollowsANewConversation(t *testing.T) {
	home := t.TempDir()
	first, firstSeg := vibeSession(t, home, "first", 2*time.Minute)
	env := harness.Env{Cwd: "/w/proj", Agent: "vibe", Vars: map[string]string{source.VarHome: home}}
	env.Procs = heldProcs{tree: []int{10, 11}}
	yes, no := func() bool { return true }, func() bool { return false }
	if got := journalFor(context.Background(), env, "", yes); got != first {
		t.Fatalf("first pick %q, want %q", got, first)
	}
	if got := journalFor(context.Background(), env, first, no); got != first {
		t.Fatalf("nothing newer: %q", got)
	}
	second, secondSeg := vibeSession(t, home, "second", time.Minute)
	if got := journalFor(context.Background(), env, first, no); got != first {
		t.Fatalf("moved with another Vibe in the folder: %q", got)
	}
	if got := journalFor(context.Background(), env, first, yes); got != second {
		t.Fatalf("the new conversation is not followed: %q", got)
	}
	// Back to the first (vibe --resume): its journal is written last.
	now := time.Now()
	_ = os.Chtimes(firstSeg, now, now)
	if got := journalFor(context.Background(), env, second, yes); got != first {
		t.Fatalf("the resume is not followed: %q", got)
	}
	// A newer journal another process holds is never taken; one the
	// session's own process holds is, whoever else runs.
	later := now.Add(time.Second)
	_ = os.Chtimes(secondSeg, later, later)
	env.Procs = heldProcs{tree: []int{10, 11}, holders: map[string][]int{secondSeg: {99}}}
	if got := journalFor(context.Background(), env, first, yes); got != first {
		t.Fatalf("took a journal another process holds: %q", got)
	}
	env.Procs = heldProcs{tree: []int{10, 11}, holders: map[string][]int{secondSeg: {11}}}
	if got := journalFor(context.Background(), env, first, no); got != second {
		t.Fatalf("the journal the session's process holds is not followed: %q", got)
	}
}

// When the journal the feed reads moves to another Vibe session, the turn
// left open in the first ends interrupted and nothing of it goes on: a
// turn id of the new session opens its own turn.
func TestSessionChangeResetsTheMapper(t *testing.T) {
	m := newTestSession(t, t.TempDir())
	b, err := os.ReadFile(filepath.Join("testdata", "journal-tool.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	var opened string // the first projection_delta: the turn starts
	for _, l := range lines {
		if strings.Contains(l, `"projection_delta"`) {
			opened = l
			break
		}
	}
	first := "turn-e7625568e7604aa7572a9af30a425266"
	m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(opened)})
	if m.State()["working"] != "true" {
		t.Fatalf("the first turn is not running: %v", m.State())
	}
	other := strings.ReplaceAll(strings.ReplaceAll(opened, "c62e7a44-5a4f-1af0-5ad4-d1c9fa135f62", "d73f8b55-6a5f-4b01-9be0-e2dad0246073"), first, "turn-0000000000000000000000000000beef")
	items, _ := m.Map(harness.Record{At: time.Unix(2, 0), Data: json.RawMessage(other)})
	ended := false
	for _, it := range items {
		if it.ID == first && it.Turn != nil {
			ended = it.Turn.State == harness.StateInterrupted
		}
	}
	if !ended {
		t.Fatalf("the left conversation's turn did not end interrupted: %+v", items)
	}
	if _, ok := m.turns[first]; ok || len(m.order) != 1 {
		t.Fatalf("the left conversation's turns are kept: %v", m.order)
	}
	if n := m.State()[harness.StateMoves]; n != "1" {
		t.Fatalf("moves %q after a move, want 1: the engine ends what the left conversation had", n)
	}
}

// The review's sequence: the pane moves at t=0 while alone; at t=3 a
// second Vibe starts in the folder and writes its journal. Each look asks
// the process view afresh (hookstyle guess.go, Solo.Check), so the second
// Vibe is seen and the chat stays.
func TestJournalForDoesNotFollowASecondVibeAfterAMove(t *testing.T) {
	home := t.TempDir()
	first, _ := vibeSession(t, home, "first", 3*time.Minute)
	env := harness.Env{Cwd: "/w/proj", Agent: "vibe", Vars: map[string]string{source.VarHome: home}}
	procs := &flipProcs{}
	env.Procs = procs
	s := &source.Solo{Also: []string{"vibe-acp", "vibe-app-server"}}
	alone := func() bool { return s.Check(env) }
	second, _ := vibeSession(t, home, "second", 2*time.Minute)
	if got := journalFor(context.Background(), env, first, alone); got != second {
		t.Fatalf("t=0: %q, want the new conversation", got)
	}
	procs.others.Store(true)
	vibeSession(t, home, "third", time.Minute)
	if got := journalFor(context.Background(), env, second, alone); got != second {
		t.Fatalf("followed the second Vibe's journal: %q", got)
	}
}

type flipProcs struct{ others atomic.Bool }

func (*flipProcs) Tree(context.Context) []int { return []int{10} }
func (*flipProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (p *flipProcs) Others(context.Context, []string, string) (bool, error) {
	return p.others.Load(), nil
}
