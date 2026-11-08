package push

import (
	"net/http"
	"testing"
	"time"
)

// A relay that fails every attempt gets one try per push for a minute, so
// an outage does not hold the push goroutine ~33 s per device per event
// (the queue then dropped events, clears included). A success restores
// the retries.
func TestRelayOutageGetsOneTry(t *testing.T) {
	e := newEnv(t, 1)
	e.relay.status.Store(http.StatusServiceUnavailable)
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	first := len(e.relay.received())
	if first != maxAttempts {
		t.Fatalf("first push: %d attempts, want %d", first, maxAttempts)
	}
	e.clock = e.clock.Add(time.Second)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := len(e.relay.received()) - first; n != 1 {
		t.Fatalf("while down: %d attempts, want 1", n)
	}
	e.relay.status.Store(http.StatusAccepted)
	e.clock = e.clock.Add(time.Second)
	e.handle(approvalOpen("r_cccccccccc"))
	before := len(e.relay.received())
	e.relay.status.Store(http.StatusServiceUnavailable)
	e.clock = e.clock.Add(time.Second)
	e.handle(approvalOpen("r_dddddddddd"))
	if n := len(e.relay.received()) - before; n != maxAttempts {
		t.Fatalf("after recovery: %d attempts, want %d", n, maxAttempts)
	}
}
