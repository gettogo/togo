package engine

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// An agent whose mapper carries on across conversations (Vibe, Goose,
// Crush, Cline) moved to another one. The chat starts over on it, as for
// an agent that names its conversation: a new epoch that holds only the
// conversation followed, its open question closed so no answer is typed
// into a pane that shows another conversation, the waiting prompt reported
// instead of sent. The mapper stays. The same count again, or a fresh
// mapper's zero after a helper restart, changes nothing.
func TestFollowingAnotherConversationStartsTheChatOver(t *testing.T) {
	const id = "s_fakemoves1"
	e, _ := newFake(t, id, "1.2.3")
	var epochs atomic.Int64
	e.log.NewEpoch = func() string { return "m" + strconv.FormatInt(epochs.Add(1), 10) }
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
	queue(t, e, id, "for the first conversation")
	epoch := e.log.Epoch(id)

	deliver(id, map[string]any{"state": map[string]string{harness.StateMoves: "1", "working": "false"},
		"items": []harness.Item{{ID: "u2", TurnID: "t2", Kind: harness.KindUser, User: &harness.UserBody{Text: "in the second"}}}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "u2"); return ok && e.log.Epoch(id) != epoch })
	if open("q1") {
		t.Fatal("the left conversation's question is still open")
	}
	for _, gone := range []string{"u1", "t1", "q1"} {
		if _, ok := e.log.Get(id, gone); ok {
			t.Fatalf("%s of the left conversation is still in the chat", gone)
		}
	}
	notice := false
	items, _, _ := e.log.Since(id, 0, 50)
	for _, it := range items {
		if it.Notice != nil && strings.Contains(it.Notice.Text, "for the first conversation") {
			notice = true
		}
		if it.User != nil && it.User.Text == "for the first conversation" {
			t.Fatal("the waiting prompt was kept for the new conversation")
		}
	}
	if !notice {
		t.Fatal("no notice carries the prompt that was not sent")
	}
	// The same count again changes nothing.
	second := e.log.Epoch(id)
	q2 := harness.Item{ID: "q2", TurnID: "t2", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{{ID: "a", Prompt: "Now?", AllowText: true}}}}
	deliver(id, map[string]any{"state": map[string]string{harness.StateMoves: "1"}, "items": []harness.Item{q2}})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "q2"); return ok })
	if !open("q2") {
		t.Fatal("a question of the conversation followed was closed")
	}
	if _, ok := e.log.Get(id, "u2"); !ok || e.log.Epoch(id) != second {
		t.Fatal("the chat started over again on a count it had seen")
	}
}
