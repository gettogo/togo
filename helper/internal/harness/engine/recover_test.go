package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A feed that failed three times took the chat away; when it delivers a
// record again the chat comes back, without waiting for a new session.
func TestCapabilitiesReturnWhenTheFeedDeliversAgain(t *testing.T) {
	id := "s_recover0001"
	var status atomic.Value
	status.Store("working")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
	s, _ := e.served(context.Background(), id)
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	// Learn which feed the fake adapter's records arrive on.
	deliver(id, map[string]any{"items": []harness.Item{{ID: "n0", Kind: harness.KindNotice, Notice: &harness.NoticeBody{Text: "first"}}}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "n0"); return ok })

	s.mu.Lock()
	if len(s.feeds) == 0 {
		s.mu.Unlock()
		t.Fatal("the session has no feed")
	}
	caps := []string{harness.CapHistory, harness.CapChatStream}
	for _, run := range s.feeds {
		run.failures, run.down = 3, caps
	}
	s.degradeLocked(caps)
	down := len(s.degradedList())
	s.mu.Unlock()
	if down == 0 {
		t.Skip("the fake adapter declares neither chat capability")
	}

	deliver(id, map[string]any{"items": []harness.Item{{ID: "n1", Kind: harness.KindNotice, Notice: &harness.NoticeBody{Text: "back"}}}})
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.degradedList()) == 0
	})
}
