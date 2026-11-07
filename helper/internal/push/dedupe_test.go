package push

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// An approval re-announced under the same request id (the hook adopting
// the screen prompt, the watcher re-reading it) must not ring again.
func TestNeedsYouPushedOncePerRequest(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("relay got %d pushes for one request, want 1", n)
	}
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := len(e.relay.received()); n != 2 {
		t.Fatalf("a new request should push: got %d", n)
	}
}

// A request that was closed never notifies again, even if a late open for
// it arrives.
func TestClosedRequestNeverRenotifies(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: wire.ResolutionDesk}})
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.relay.received()
	if len(got) != 1 || got[0].Kind != relayBackground {
		t.Fatalf("relay got %+v, want only the clear", got)
	}
}

// A request skipped while the app was in the foreground may still ring
// once later: the user has not been told about it on the lock screen.
func TestNeedsYouSkippedInForegroundCanPushLater(t *testing.T) {
	e := newEnv(t, 1)
	e.hub.fg.Store(1)
	e.handle(approvalOpen("r_abcdefghij"))
	e.hub.fg.Store(0)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("relay got %d pushes, want 1", n)
	}
}

func TestRequestMemoryIsBounded(t *testing.T) {
	var d requestSeen
	for i := 0; i < maxSeenRequests*3; i++ {
		d.mark(sprintf("r_%010d", i), seenPushed)
	}
	if len(d.m) > maxSeenRequests {
		t.Fatalf("remembered %d requests, cap %d", len(d.m), maxSeenRequests)
	}
	if d.blocks(sprintf("r_%010d", maxSeenRequests*3-1)) == false {
		t.Fatal("the newest request must still be remembered")
	}
}
