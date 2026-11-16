package watch

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

// A reconcile still holding an older listing must not announce as removed
// a session that a newer reconcile has already taken in.
func TestOverlappingReconcilesKeepANewSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	const fresh = "s_newnewnewx"
	inList, release := make(chan struct{}), make(chan struct{})
	calls := 0
	h.w.list = func(context.Context) ([]session.Session, error) {
		h.mu.Lock()
		calls++
		n := calls
		h.mu.Unlock()
		old := []session.Session{{ID: sid, Name: "api", Managed: true}}
		if n == 1 {
			close(inList)
			<-release
			return old, nil
		}
		return append(old, session.Session{ID: fresh, Name: "new", Managed: true}), nil
	}
	first, second := make(chan struct{}), make(chan struct{})
	go func() { h.w.Reconcile(ctx); close(first) }()
	<-inList
	go func() { h.w.Reconcile(ctx); close(second) }()
	select {
	case <-second:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-first
	<-second
	h.mu.Lock()
	removed := append([]string(nil), h.removed...)
	h.mu.Unlock()
	if len(removed) != 0 {
		t.Fatalf("announced %v as removed", removed)
	}
	h.w.mu.Lock()
	_, ok := h.w.sessions[fresh]
	h.w.mu.Unlock()
	if !ok {
		t.Fatal("the new session is not watched")
	}
}
