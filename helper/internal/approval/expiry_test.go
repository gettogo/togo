package approval

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("never: %s", what)
}

// opensOf counts how many times the approval was announced as open.
func opensOf(pub *fakePub, requestID string) int {
	pub.mu.Lock()
	defer pub.mu.Unlock()
	n := 0
	for _, e := range pub.events {
		if o, ok := e.payload.(wire.ApprovalOpenEvent); ok && o.Approval.RequestID == requestID {
			n++
		}
	}
	return n
}

// A prompt nobody answered within the TTL is still blocking the agent: its
// approval stays answerable, with a later expiry, until the prompt leaves.
func TestApprovalOutlivesItsTTLWhileThePromptIsShown(t *testing.T) {
	ctx := context.Background()
	const id = "s_aaaaaaaaaa"
	tm := &fakeTmux{screen: menu}
	pub := &fakePub{}
	st := New(Config{Tmux: tm, Pub: pub, TTL: 60 * time.Millisecond})
	p, ok := detect.Detect(menu)
	if !ok {
		t.Fatal("the menu is not detected")
	}
	st.PromptSeen(ctx, id, p)
	first := st.Pending(id)[0]

	// The first TTL has passed once the extended expiry is announced again.
	eventually(t, "the extended expiry announced", func() bool { return opensOf(pub, first.RequestID) >= 2 })
	if got := pub.closed(); len(got) != 0 {
		t.Fatalf("closed %v while the prompt is on screen", got)
	}
	now := st.Pending(id)
	if len(now) != 1 || now[0].RequestID != first.RequestID {
		t.Fatalf("pending = %+v, want the same request", now)
	}
	// Timestamps on the wire are whole seconds: the new expiry shows as
	// the approval being announced again, never as an earlier time.
	if now[0].ExpiresAt.Before(*first.ExpiresAt) {
		t.Fatal("the expiry moved back")
	}

	tm.mu.Lock()
	tm.screen = "the agent moved on\n"
	tm.mu.Unlock()
	eventually(t, "expired once the prompt left", func() bool {
		got := pub.closed()
		return len(got) == 1 && got[0] == wire.ResolutionExpired
	})
}

// An ask waits as long as its caller said, shorter or longer than the TTL.
func TestAskLivesForItsOwnTimeout(t *testing.T) {
	pub := &fakePub{}
	st := New(Config{Tmux: &fakeTmux{}, Pub: pub, TTL: 40 * time.Millisecond})
	a, _ := st.OpenAsk(context.Background(), wire.AskParams{Title: "Deploy?", Options: []string{"Yes", "No"}, TimeoutSec: 2})
	if got := a.ExpiresAt.Sub(a.CreatedAt); got != 2*time.Second {
		t.Fatalf("expires after %v, want 2s", got)
	}
	time.Sleep(200 * time.Millisecond)
	if got := pub.closed(); len(got) != 0 {
		t.Fatalf("closed %v before the caller's timeout", got)
	}
	if len(st.Pending("")) != 1 {
		t.Fatal("the ask is no longer pending")
	}
}
