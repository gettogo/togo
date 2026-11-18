package push

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// The push and the Live Activity context say when the request stops taking answers.
func TestNeedsYouCarriesItsExpiry(t *testing.T) {
	e := newEnv(t, 1)
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	until := e.clock.Add(25 * time.Minute)
	a.Approval.ExpiresAt = &until
	ev.Payload = a
	m, ok := e.sender.message(ev, "s_aaaaaaaaaa", SessionInfo{})
	if !ok || m.Plaintext.Expires != until.Unix() {
		t.Fatalf("push expires = %d, ok %v; want %d", m.Plaintext.Expires, ok, until.Unix())
	}
	if c := requestContext(a.Approval); c.Expires != until.Unix() {
		t.Fatalf("context expires = %d, want %d", c.Expires, until.Unix())
	}
	later := until.Add(10 * time.Minute)
	b := a.Approval
	b.ExpiresAt = &later
	if requestContext(a.Approval).equal(requestContext(b)) {
		t.Fatal("a later expiry does not count as a change, so the activity would keep the old one")
	}
}
