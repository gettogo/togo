package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// An agent that reports no conversation of its own (Pi, Kimi Code, Vibe,
// Goose, Crush, Cline) moved to another one and the chat keeps the
// earlier one above it. What belonged to the conversation left ends: its
// open question closes, so an answer is not typed into a pane that shows
// another conversation; a prompt waiting to be sent is withdrawn; the
// running turn ends. The chat keeps its items and its epoch.
func TestFollowingAnotherConversationEndsWhatTheLeftOneHad(t *testing.T) {
	const id = "s_fakemoves1"
	e, _ := newFake(t, id, "1.2.3")
	var status atomic.Value
	status.Store("working")
	e.cfg.Status = func(string) string { v, _ := status.Load().(string); return v }
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	ask := harness.Item{ID: "q1", TurnID: "t1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{{ID: "a", Prompt: "Which one?", AllowText: true}}}}
	turn := harness.Item{ID: "t1", TurnID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateRunning}}
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}, "items": []harness.Item{
		{ID: "u1", TurnID: "t1", Kind: harness.KindUser, User: &harness.UserBody{Text: "in the first"}}, turn, ask}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "q1"); return ok })
	open := func(q string) bool {
		e.mu.Lock()
		s := e.sessions[id]
		e.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		_, ok := s.needs.open[q]
		return ok
	}
	if !open("q1") {
		t.Fatal("the question is not open before the move")
	}
	queued := queue(t, e, id, "for the first conversation")
	epoch := e.log.Epoch(id)

	deliver(id, map[string]any{"state": map[string]string{harness.StateMoves: "1", "working": "false"},
		"items": []harness.Item{{ID: "u2", TurnID: "t2", Kind: harness.KindUser, User: &harness.UserBody{Text: "in the second"}}}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "u2"); return ok })
	waitFor(t, func() bool { return !open("q1") })
	if q, _ := e.log.Get(id, queued); q.User == nil || !q.User.Withdrawn {
		t.Fatalf("the queued prompt is %+v, want withdrawn", q.User)
	}
	if tr, _ := e.log.Get(id, "t1"); tr.Turn == nil || tr.Turn.State != harness.StateInterrupted {
		t.Fatalf("the left conversation's turn is %+v", tr.Turn)
	}
	if _, ok := e.log.Get(id, "u1"); !ok || e.log.Epoch(id) != epoch {
		t.Fatal("the chat started over: the keep-both convention is the adapter's")
	}
	// The same count again changes nothing.
	q2 := harness.Item{ID: "q2", TurnID: "t2", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{{ID: "a", Prompt: "Now?", AllowText: true}}}}
	deliver(id, map[string]any{"state": map[string]string{harness.StateMoves: "1"}, "items": []harness.Item{q2}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "q2"); return ok })
	if !open("q2") {
		t.Fatal("a question of the conversation followed was closed")
	}
}
