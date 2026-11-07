package watch

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/tap"
	"github.com/gettogo/togo/helper/internal/wire"
)

// A shell command mark goes to the harness feeds and changes no status.
func TestShellMarkReachesHarness(t *testing.T) {
	h := newHarness(t)
	var got []string
	h.w.cfg.Shell = func(id string, body []byte) { got = append(got, id+" "+string(body)) }
	code := 2
	sig := wire.Signal{SessionID: sid, Kind: tap.SignalShell, Title: tap.MarkEnd, Body: `{"mark":"D","exit":2}`, Code: &code}
	if err := h.w.Signal(context.Background(), sig); err != nil {
		t.Fatal(err)
	}
	sig.Body = "{bad"
	if err := h.w.Signal(context.Background(), sig); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != sid+` {"mark":"D","exit":2}` {
		t.Fatalf("delivered %q", got)
	}
	if len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("a mark changed status %q or notified %v", h.statuses, h.events)
	}
}
