package push

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// A request opened again after a helper restart was already pushed by the
// helper before it: it does not ring again, then or on a later update.
func TestRestoredRequestDoesNotRingAgain(t *testing.T) {
	e := newEnv(t, 1)
	ev := approvalOpen("r_restored01")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Restored = true
	ev.Payload = a
	if _, ok := e.sender.message(ev, "s_aaaaaaaaaa", SessionInfo{}); ok {
		t.Fatal("a restored request rang again")
	}
	if _, ok := e.sender.message(approvalOpen("r_restored01"), "s_aaaaaaaaaa", SessionInfo{}); ok {
		t.Fatal("an update of a restored request rang again")
	}
	if _, ok := e.sender.message(approvalOpen("r_fresh00001"), "s_aaaaaaaaaa", SessionInfo{}); !ok {
		t.Fatal("a new request did not ring")
	}
}
