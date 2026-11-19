package e2e

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Codex's approvals and questions reach the helper as the app-server's
// server requests, the same ones its terminal gets. The phone answers
// over the request (rpc_reply); the first answer, from either side, wins.

// approvalTurn writes a turn whose file change waits on an approval and
// holds back what follows the request. It returns the request's id and
// the release.
func (c *codexSession) approvalTurn(t *testing.T) (int, func()) {
	t.Helper()
	c.p.Approval = true
	var id int
	release := c.hold(t, sessiongen.Turn{Say: 20}, func(m map[string]any) bool {
		if m["method"] == "item/fileChange/requestApproval" {
			id = int(m["id"].(float64))
			return true
		}
		return false
	})
	return id, release
}

// reply waits for the answer to request id and returns its result.
func (c *codexSession) reply(t *testing.T, id int) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(quick); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, r := range c.s.Replies() {
			if string(r.ID) == itoa(id) {
				var out map[string]any
				_ = json.Unmarshal(r.Params, &out)
				return out
			}
		}
	}
	t.Fatalf("no answer to request %d: %+v", id, c.s.Replies())
	return nil
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// An approval answered from the phone, one answered at the desk first,
// and two back to back with the same wording: each phone answer goes to
// its own request as {"decision": "accept"}, each approval closes once,
// by whoever answered it, and a later answer is refused and never sent.
func TestCodexApprovals(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/capability/needs.approvals")
	c := codexStart(t, 81, nil, sessiongen.Turn{Say: 20})
	settleUntil(c.r, 1)
	c.joined(t)

	// From the phone.
	id, release := c.approvalTurn(t)
	p := c.r.WaitPending(1, quick)[0]
	st := c.r.SettleUntil(quick, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou > 0 })
	waiting := false
	for _, it := range items(st, ofKind(harness.KindTool)) {
		waiting = waiting || it.Tool.State == harness.StateWaiting
	}
	if !waiting {
		t.Error("the file change is not shown waiting on the approval")
	}
	if err := c.r.Answer(p.RequestID, wire.DecisionApprove); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got := c.reply(t, id); got["decision"] != "accept" || len(got) != 1 {
		t.Errorf("the answer sent %v, want {decision: accept}", got)
	}
	release()
	c.r.SettleUntil(quick, func(st enginetest.State) bool { return hasPrompt(2)(st) && st.Mission.Summary.NeedsYou == 0 })
	if got := c.r.Closed()[p.RequestID]; got != wire.ByPhone {
		t.Errorf("closed as %q, want %q", got, wire.ByPhone)
	}
	if err := c.r.Answer(p.RequestID, wire.DecisionDeny); err == nil {
		t.Error("a second answer was taken")
	}

	// At the desk first.
	id, release = c.approvalTurn(t)
	p = c.r.WaitPending(1, quick)[0]
	if !c.s.AnswerAtDesk(id) {
		t.Fatal("the request was not open at the desk")
	}
	eventually(t, quick, "the approval to close", func() bool { return len(c.r.Pending()) == 0 })
	replies := len(c.s.Replies())
	if err := c.r.Answer(p.RequestID, wire.DecisionApprove); err == nil {
		t.Error("the phone answered an approval the desk had answered")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(c.s.Replies()); n != replies {
		t.Errorf("an answer was sent for a request the desk answered: %+v", c.s.Replies()[replies:])
	}
	release()
	c.r.SettleUntil(quick, func(st enginetest.State) bool { return hasPrompt(3)(st) && !st.Mission.Summary.Working })

	// Two back to back, the same words.
	first, release := c.approvalTurn(t)
	a := c.r.WaitPending(1, quick)[0]
	if err := c.r.Answer(a.RequestID, wire.DecisionApprove); err != nil {
		t.Fatal(err)
	}
	c.reply(t, first)
	release()
	second, release := c.approvalTurn(t)
	b := c.r.SettleUntil(quick, func(enginetest.State) bool {
		ps := c.r.Pending()
		return len(ps) == 1 && ps[0].RequestID != a.RequestID
	})
	_ = b
	bp := c.r.Pending()[0]
	if bp.Title != a.Title {
		t.Errorf("the two approvals read %q and %q, the test wants the same wording", a.Title, bp.Title)
	}
	if err := c.r.Answer(bp.RequestID, wire.DecisionDeny); err != nil {
		t.Fatalf("answer the second: %v", err)
	}
	if got := c.reply(t, second); got["decision"] != "decline" {
		t.Errorf("the second answer sent %v", got)
	}
	release()
	st = c.r.SettleUntil(quick, func(st enginetest.State) bool { return hasPrompt(5)(st) && !st.Mission.Summary.Working })
	if n := len(c.s.Replies()); n != 3 {
		t.Errorf("%d answers sent, want 3: %+v", n, c.s.Replies())
	}
	if len(c.s.Pending()) != 0 || len(c.r.Pending()) != 0 {
		t.Errorf("requests still open: %+v %+v", c.s.Pending(), c.r.Pending())
	}
	nothingRuns(t, st)
}

// A question from the request_user_input tool, answered from the phone
// with one of its options: the answer is sent as the tool takes it, keyed
// by the question's id, and the question closes.
func TestCodexQuestion(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/capability/needs.questions")
	c := codexStart(t, 83, nil, sessiongen.Turn{Say: 20})
	settleUntil(c.r, 1)
	c.joined(t)
	c.p.Ask = true
	var id int
	release := c.hold(t, sessiongen.Turn{Say: 20}, func(m map[string]any) bool {
		if m["method"] == "item/tool/requestUserInput" {
			id = int(m["id"].(float64))
			return true
		}
		return false
	})
	st := c.r.SettleUntil(quick, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou > 0 })
	var q harness.Item
	for _, it := range items(st, ofKind(harness.KindQuestion)) {
		q = it
	}
	if q.Question == nil || len(q.Question.Questions) != 1 || len(q.Question.Questions[0].Options) != 2 {
		t.Fatalf("question %+v", q.Question)
	}
	res, err := c.r.Act("answer", q.ID, map[string]any{"answers": map[string][]string{"scope": {"Only Add"}}})
	if err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("answer: %+v %v", res, err)
	}
	got, _ := json.Marshal(c.reply(t, id))
	if string(got) != `{"answers":{"scope":{"answers":["Only Add"]}}}` {
		t.Errorf("the answer sent %s", got)
	}
	release()
	st = c.r.SettleUntil(quick, func(st enginetest.State) bool { return hasPrompt(2)(st) && !st.Mission.Summary.Working })
	if st.Mission.Summary.NeedsYou != 0 {
		t.Error("still needs the user after the answer")
	}
	if it, _ := latestOf(st, q.ID); it.Question == nil || it.Question.Answered == nil {
		t.Errorf("the question is not closed: %+v", it.Question)
	}
}
