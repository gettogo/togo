package codex

import (
	"errors"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

// TestKnownFault_Codex_LongWaitApprovalRefused: an approval left
// waiting past its lifetime on the phone (the store's TTL, 10 minutes by
// default), while Codex still waits on the request: the phone still lists
// one approval for it, and answering that approval is refused as a
// conflict (-32006); nothing reaches Codex. The approval is an ask the
// engine offers for the app-server's question (engine/feedapproval.go:
// 49-74), and an ask is never extended at its lifetime as a screen
// prompt is (approval/expiry.go:34-40). The question card in the chat
// still answers (harness.action answer): only the approval, the one the
// Lock Screen and the Watch answer, is dead.
//
// The engine offers the ask again under a new id after each lifetime; a
// fresh offer answers, one listed past its lifetime is refused.
//
// Correct: while Codex waits, the approval the phone lists answers the
// request.
func TestKnownFault_Codex_LongWaitApprovalRefused(t *testing.T) {
	t.Parallel()
	const ttl = 300 * time.Millisecond
	c := codexStart(t, 87, func(o *enginetest.Options) { o.ApprovalTTL = ttl }, sessiongen.Turn{Say: 20})
	support.SettleUntil(c.r, 1)
	c.joined(t)
	_, release := c.approvalTurn(t)
	defer release()
	c.r.WaitPending(1, support.Quick)
	// Past its lifetime the ask is offered again under a new id, which
	// answers until its own lifetime ends: wait until the one listed is
	// older than its lifetime, whichever offer it is. (A fixed wait
	// sampled either phase, and the test passed or failed by it.)
	var ps []wire.Approval
	stale := func() bool {
		ps = c.r.Pending()
		return len(ps) == 1 && time.Since(ps[0].CreatedAt) > 2*ttl
	}
	for deadline := time.Now().Add(30 * time.Second); !stale() && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if !stale() {
		t.Fatalf("no approval listed past its lifetime: the fault changed: %+v", ps)
	}
	err := c.r.Answer(ps[0].RequestID, wire.DecisionApprove)
	if err == nil {
		t.Fatal("the listed approval took the answer: the fault is fixed; claim the long wait in TestCodexApprovals")
	}
	if !busy(err) {
		t.Fatalf("the fault changed: the answer gave %v, the fault gives a conflict", err)
	}
	time.Sleep(200 * time.Millisecond)
	if r := c.s.Replies(); len(r) != 0 {
		t.Errorf("answers were sent: %+v", r)
	}
}

// The helper's connection to the app-server drops, or the helper
// restarts, while Codex waits on an approval. The recordings show each
// server request sent once, to the clients there when it was made (the
// test server sends no request again), and the helper drops a question
// whose connection ended (adapters/codex/live.go:623): the approval
// closes, the phone's answer is refused and nothing is sent, so nothing
// is shown as answered that Codex did not get. Codex's terminal still
// has the request.
func TestCodexApprovalOnALostConnection(t *testing.T) {
	t.Parallel()
	for _, how := range []string{"drop", "restart"} {
		t.Run(how, func(t *testing.T) {
			t.Parallel()
			c := codexStart(t, 89, nil, sessiongen.Turn{Say: 20})
			support.SettleUntil(c.r, 1)
			c.joined(t)
			id, release := c.approvalTurn(t)
			p := c.r.WaitPending(1, support.Quick)[0]
			resumes := len(c.s.CallsOf("thread/resume"))
			if how == "drop" {
				c.s.DropConns()
			} else {
				c.r.Restart()
			}
			support.Eventually(t, support.Quick, "the helper to join again", func() bool { return len(c.s.CallsOf("thread/resume")) > resumes })
			st := c.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return len(c.r.Pending()) == 0 })
			if err := c.r.Answer(p.RequestID, wire.DecisionApprove); err == nil {
				t.Error("an approval whose request went with the connection took an answer")
			}
			for _, it := range support.Items(st, support.OfKind(harness.KindQuestion)) {
				if res, _ := c.r.Act("answer", it.ID, map[string]any{"answers": map[string][]string{"decision": {"accept"}}}); res.Result == harness.ResultApplied {
					t.Errorf("the question %s took an answer after its request went", it.ID)
				}
			}
			time.Sleep(200 * time.Millisecond)
			if r := c.s.Replies(); len(r) != 0 {
				t.Errorf("answers were sent: %+v", r)
			}
			if len(c.s.Pending()) != 1 {
				t.Errorf("Codex's request %d is no longer open: %+v", id, c.s.Pending())
			}
			// Answered in the terminal, the turn goes on and ends.
			c.s.AnswerAtDesk(id)
			release()
			c.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return support.HasPrompt(2)(st) && !st.Mission.Summary.Working })
		})
	}
}

func busy(err error) bool {
	var ce *api.CodeError
	return errors.As(err, &ce) && ce.Code == rpc.CodeConflict
}
