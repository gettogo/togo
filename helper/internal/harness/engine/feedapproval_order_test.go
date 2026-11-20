package engine

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// gatedAsks is askingApprovals whose OpenAsk, once the ask is pending,
// waits for release: the store has published the open event, and the
// engine's offer has not learnt the request id yet.
type gatedAsks struct {
	askingApprovals
	opened  chan wire.Approval
	release chan struct{}
}

func (g *gatedAsks) OpenAsk(ctx context.Context, p wire.AskParams) (wire.Approval, <-chan string) {
	a, ch := g.askingApprovals.OpenAsk(ctx, p)
	g.opened <- a
	<-g.release
	return a, ch
}

// A question answered at the desk while the engine's offer of it opens: the
// ask's open event finds no open question to own it. The offer is the
// engine's own copy of a question that is gone, so it is not drawn as an
// approval of its own, with a Needs row, and then closed; the offer closes
// it at the desk and the chat keeps only the answered question. Found by
// the end-to-end suite on Copilot, where the card was lost at a restart.
func TestOfferAnsweredWhileOpeningIsNotDrawn(t *testing.T) {
	const id = "s_permord1"
	ap := &gatedAsks{opened: make(chan wire.Approval, 1), release: make(chan struct{})}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	defer func() {
		select {
		case <-ap.release:
		default:
			close(ap.release)
		}
	}()

	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", nil)}, nil)
	s.mu.Unlock()
	var a wire.Approval
	select {
	case a = <-ap.opened:
	case <-time.After(30 * time.Second):
		t.Fatal("the question was never offered")
	}
	// Answered at the desk, then the ask's open event is handled.
	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", map[string][]string{"0": {"once"}})}, nil)
	s.mu.Unlock()
	e.addApproval(s, a)
	if it, ok := e.log.Get(id, "approval-"+a.RequestID); ok {
		t.Fatalf("the offer of an answered question was drawn: %+v", it.Approval)
	}
	close(ap.release)
	waitFor(t, func() bool { _, _, closed, _ := ap.snapshot(); return len(closed) == 1 && closed[0] == a.RequestID })
	if _, ok := e.log.Get(id, "approval-"+a.RequestID); ok {
		t.Fatal("the offer was drawn after it closed")
	}
	s.mu.Lock()
	left := len(s.needs.opening)
	s.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d offers still noted as opening", left)
	}
}

// An ask of the same title that is not a dropped offer (togo ask, or the
// offer of a question still open) is drawn as before.
func TestOfferOpeningForAnOpenQuestionIsDrawn(t *testing.T) {
	const id = "s_permord2"
	ap := &gatedAsks{opened: make(chan wire.Approval, 1), release: make(chan struct{})}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	defer close(ap.release)

	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", nil)}, nil)
	s.mu.Unlock()
	var a wire.Approval
	select {
	case a = <-ap.opened:
	case <-time.After(30 * time.Second):
		t.Fatal("the question was never offered")
	}
	e.addApproval(s, a)
	it, ok := e.log.Get(id, "approval-"+a.RequestID)
	if !ok || it.Approval.Owner != "per_1~ses_1" {
		t.Fatalf("the offer of an open question = %+v %v", it.Approval, ok)
	}
}
