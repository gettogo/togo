package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Changes and Ports refresh on their period only while a chat has the
// session open; otherwise rarely.
func TestBuiltinsBackOffWhileUnwatched(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &sess{ctx: ctx, subs: map[harness.Subscriber]*subQueue{}}
	var n atomic.Int32
	go (&Engine{}).every(s, 10*time.Millisecond, func(context.Context) { n.Add(1) })
	// The first refresh runs at once; then the unwatched session is left
	// alone for a long while, so a short hold shows no second one.
	for deadline := time.Now().Add(30 * time.Second); n.Load() < 1; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no refresh ran")
		}
	}
	time.Sleep(150 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Fatalf("%d refreshes unwatched, want 1", got)
	}
	s.mu.Lock()
	s.subs[&fakeSub{done: make(chan struct{})}] = nil
	s.mu.Unlock()
	for deadline := time.Now().Add(30 * time.Second); n.Load() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d refreshes watched", n.Load())
		}
	}
}
