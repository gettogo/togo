package mailbox

import (
	"testing"
	"time"
)

func TestLinkGapTellsWhenTheWillMayHaveGone(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	g := &linkGap{}
	if g.willSent(at) {
		t.Fatal("first connection treated as a return")
	}
	g.closed(at, -1)
	if g.willSent(at.Add(30 * time.Second)) {
		t.Fatal("a short drop treated as a return from silence")
	}
	g.closed(at, -1)
	if !g.willSent(at.Add(silentGap)) {
		t.Fatal("a long gap not treated as silence")
	}
	if g.willSent(at.Add(time.Hour)) {
		t.Fatal("record not cleared after it was read")
	}
	g.closed(at, StatusSilent)
	if !g.willSent(at.Add(time.Second)) {
		t.Fatal("the relay's silent close not treated as silence")
	}
}
