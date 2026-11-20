package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/panelock"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// noServer is a typist whose tmux has no server: every key fails at once,
// which is enough to see whether a call waited for the pane first.
func noServer() SessionTypist {
	return SessionTypist{Tmux: &tmux.Client{Bin: "tmux", Socket: []string{"-L", "togo-no-such-server"}}}
}

// A stop goes through at once while another write (a form answer, a
// prompt) holds the pane, and that write sends nothing after it.
func TestStopKeysDoNotWaitForThePane(t *testing.T) {
	const id = "s_stopnowabc"
	writing, release, err := panelock.Acquire(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan struct{})
	go func() {
		_ = noServer().StopKeys(context.Background(), id, "Escape")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop waited behind the write holding the pane")
	}
	if !panelock.Stopped(writing, id, 0) {
		t.Fatal("the write holding the pane did not see the stop")
	}
	if err := noServer().Keys(writing, id, "Enter"); err != errStopped {
		t.Fatalf("a key of the stopped write: %v, want errStopped", err)
	}
}

// An engine sequence holds the pane from its first key to its last: a
// form answer waiting for the pane comes after the Enter, not between the
// paste and the Enter. Its own typist calls do not wait for it.
func TestEngineSequenceHoldsThePaneThroughEnter(t *testing.T) {
	e := &Engine{}
	s := &sess{id: "s_seqholdabc"}
	ctx, release, err := e.paneSeq(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []string
	note := func(w string) { mu.Lock(); order = append(order, w); mu.Unlock() }
	answered := make(chan struct{})
	go func() {
		_, rel, err := panelock.Acquire(context.Background(), s.id) // what the form driver takes
		if err == nil {
			note("answer")
			rel()
		}
		close(answered)
	}()
	note("paste")
	keyed := make(chan struct{})
	go func() { _ = noServer().Keys(ctx, s.id, "Enter"); close(keyed) }()
	select {
	case <-keyed:
	case <-time.After(5 * time.Second):
		t.Fatal("the sequence's own Enter waited for the pane it holds")
	}
	note("enter")
	release()
	<-answered
	if len(order) != 3 || order[0] != "paste" || order[1] != "enter" || order[2] != "answer" {
		t.Fatalf("order %v: the answer landed inside the sequence", order)
	}
}
