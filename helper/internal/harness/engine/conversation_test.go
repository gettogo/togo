package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen live on omp 18.20, OpenCode 2.3 and others: the agent moved to a new
// conversation in its terminal and the chat kept the old conversation's
// messages, with the new replies appended to them. The chat now starts over:
// a new epoch that holds only the new conversation, a reset for open chats,
// and held prompts reported instead of sent to the wrong conversation.
func TestChatStartsOverWhenTheAgentChangesConversation(t *testing.T) {
	const id = "s_convconv01"
	e, _ := newFake(t, id, "1.2.3")
	var epochs atomic.Int64
	e.log.NewEpoch = func() string { return "e" + strconv.FormatInt(epochs.Add(1), 10) }
	var status atomic.Value
	status.Store("idle")
	e.cfg.Status = func(string) string { v, _ := status.Load().(string); return v }
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	user := func(item, text string) []harness.Item {
		return []harness.Item{{ID: item, TurnID: item, Kind: harness.KindUser, User: &harness.UserBody{Text: text}}}
	}
	deliver(id, map[string]any{"state": map[string]string{"conversation": "A"}, "items": user("a1", "in the first")})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "a1"); return ok })
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	first, err := e.Open(ctx, sub, id, 0, 50)
	if err != nil || len(first.Items) != 1 {
		t.Fatalf("open = %+v %v", first, err)
	}
	e.Release(sub, id)
	// The same conversation named again changes nothing.
	deliver(id, map[string]any{"state": map[string]string{"conversation": "A"}, "items": user("a2", "still the first")})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "a2"); return ok })
	if got := e.log.Epoch(id); got != first.Epoch {
		t.Fatalf("epoch moved to %q with no change of conversation", got)
	}
	status.Store("working")
	queue(t, e, id, "for the first conversation")

	deliver(id, map[string]any{"state": map[string]string{"conversation": "B"}, "items": user("b1", "in the second")})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "b1"); return ok && e.log.Epoch(id) != first.Epoch })
	second, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	notice := ""
	for _, it := range second.Items {
		if it.User != nil {
			texts = append(texts, it.User.Text)
		}
		if it.Notice != nil {
			notice = it.Notice.Text
		}
	}
	if len(texts) != 1 || texts[0] != "in the second" {
		t.Errorf("new chat holds %q, want only the second conversation", texts)
	}
	if !strings.Contains(notice, "for the first conversation") {
		t.Errorf("notice = %q, want it to carry the prompt that was not sent", notice)
	}
	waitFor(t, func() bool {
		for _, n := range sub.notes() {
			if n.Kind == harness.NoteReset && n.Reason == "epoch" && n.Epoch == second.Epoch {
				return true
			}
		}
		return false
	})
	// The new conversation does not switch again on its own records.
	deliver(id, map[string]any{"state": map[string]string{"conversation": "B"}, "items": user("b2", "more")})
	waitFor(t, func() bool { _, ok := e.log.Get(id, "b2"); return ok })
	if got := e.log.Epoch(id); got != second.Epoch {
		t.Errorf("epoch moved again to %q", got)
	}
}

// The terminal moves to a conversation saved in another file: its feed
// reads that file as a snapshot. The chat starts over and holds the whole
// snapshot, from its first record.
func TestSnapshotOfAnotherConversationStartsTheChatOver(t *testing.T) {
	const id = "s_convconv02"
	e, _ := newFake(t, id, "1.2.3")
	var epochs atomic.Int64
	e.log.NewEpoch = func() string { return "e" + strconv.FormatInt(epochs.Add(1), 10) }
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	rec := func(conv, item, text string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"state": map[string]string{"conversation": conv},
			"items": []harness.Item{{ID: item, TurnID: item, Kind: harness.KindUser, User: &harness.UserBody{Text: text}}}})
		return b
	}
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s.record(ctx, "fake.events", harness.Record{Source: "events", Replay: []json.RawMessage{rec("A", "a1", "one"), rec("A", "a2", "two")}})
	before := e.log.Epoch(id)
	s.record(ctx, "fake.events", harness.Record{Source: "events", Replay: []json.RawMessage{rec("B", "b1", "three"), rec("B", "b2", "four")}})
	res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, it := range res.Items {
		if it.User != nil {
			texts = append(texts, it.User.Text)
		}
	}
	if res.Epoch == before || strings.Join(texts, ",") != "three,four" {
		t.Errorf("epoch %q -> %q, chat = %q; want a new epoch holding three,four", before, res.Epoch, texts)
	}
}
