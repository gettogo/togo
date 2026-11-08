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
	time.Sleep(150 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Fatalf("%d refreshes unwatched, want 1", got)
	}
	s.mu.Lock()
	s.subs[&fakeSub{done: make(chan struct{})}] = nil
	s.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	if got := n.Load(); got < 5 {
		t.Fatalf("%d refreshes watched", got)
	}
}
