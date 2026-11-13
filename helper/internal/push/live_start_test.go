package push

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestUnconfirmedStartDoesNotSuppressNextWaitingCycle(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	old := pendingStart(e)
	e.handle(approvalClosed("r_aaaaaaaaaa"))
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	current := pendingStart(e)
	la, _ := e.livePushes()
	if len(la) != 2 || la[1].LiveActivity.Event != liveStart || *la[1].LiveActivity.ContentState.RequestID != "r_bbbbbbbbbb" {
		t.Fatalf("got %d live pushes, want a fresh start for the new waiting cycle", len(la))
	}
	if old == nil || current == nil || old == current {
		t.Fatal("the new cycle did not get its own start attempt")
	}
	// A late timeout for the old request cannot release the new start or send
	// an obsolete fallback alert.
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", old)
	if pendingStart(e) != current || len(e.relay.received()) != before {
		t.Fatal("old timeout changed the new attempt")
	}
}

func TestUnconfirmedClosedRequestReleasesItsStart(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	attempt := pendingStart(e)
	e.handle(approvalClosed("r_aaaaaaaaaa"))
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	if pendingStart(e) != nil || len(e.relay.received()) != before {
		t.Fatal("closed request kept its pending start or sent an obsolete alert")
	}
}

func TestUnconfirmedOpenRequestFallsBackWithoutRepeatedStarts(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	attempt := pendingStart(e)
	before := len(e.relay.received())
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	if pendingStart(e) != nil || len(e.relay.received()) != before+1 {
		t.Fatal("unconfirmed request did not release its start and send one fallback")
	}
	e.sender.startUnconfirmed(startMessage("r_aaaaaaaaaa"), "dev_a", attempt)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	la, _ := e.livePushes()
	if len(la) != 1 {
		t.Fatalf("same waiting cycle retried the start: %d pushes", len(la))
	}
}

func startMessage(id string) Message {
	return Message{Kind: relayAlert, Plaintext: wire.PushPlaintext{Kind: wire.PushNeedsYou, RequestID: id}}
}

func pendingStart(e *env) *liveStartAttempt {
	e.sender.liveMu.Lock()
	defer e.sender.liveMu.Unlock()
	return e.sender.liveDev["dev_a"].started
}
