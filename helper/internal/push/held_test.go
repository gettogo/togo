package push

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// The app looked foregrounded when the request opened (its lease runs 45 s
// after its last request), then the phone dropped off. Once the lease
// would have run out the request, still waiting, is pushed after all.
func TestNeedsYouHeldForTheForegroundGoesOutLater(t *testing.T) {
	old := foregroundRecheck
	foregroundRecheck = 50 * time.Millisecond
	t.Cleanup(func() { foregroundRecheck = old })

	r := newRemindEnv(t)
	r.hub.fg.Store(1)
	r.handle(approvalOpen("r_abcdefghij"))
	if n := len(r.relay.received()); n != 0 {
		t.Fatalf("%d pushes while foregrounded", n)
	}
	r.hub.fg.Store(0)
	deadline := time.Now().Add(2 * time.Second)
	for len(r.relay.received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := r.relay.received()
	if len(got) != 1 {
		t.Fatalf("%d pushes after the lease, want 1", len(got))
	}
	if p := r.open(t, got[0].Ciphertext, "dev_a"); p.Kind != wire.PushNeedsYou || p.RequestID != "r_abcdefghij" || p.Reminder {
		t.Fatalf("held push %+v", p)
	}
}

// Answered (or still in front of the user) by then: nothing goes out.
func TestHeldNeedsYouDroppedWhenAnswered(t *testing.T) {
	old := foregroundRecheck
	foregroundRecheck = 50 * time.Millisecond
	t.Cleanup(func() { foregroundRecheck = old })

	r := newRemindEnv(t)
	var answered atomic.Bool
	r.rem.Pending = func(string) bool { return !answered.Load() }
	r.hub.fg.Store(1)
	r.handle(approvalOpen("r_abcdefghij"))
	answered.Store(true)
	r.hub.fg.Store(0)
	time.Sleep(200 * time.Millisecond)
	if n := len(r.relay.received()); n != 0 {
		t.Fatalf("%d pushes for an answered request", n)
	}
}
