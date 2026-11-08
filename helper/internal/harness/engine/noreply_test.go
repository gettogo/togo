package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func noticesIn(e *Engine, id string) []harness.Item {
	items, _, _ := e.log.Since(id, 0, 200)
	var out []harness.Item
	for _, it := range items {
		if it.Kind == harness.KindNotice {
			out = append(out, it)
		}
	}
	return out
}

// Recorded on OpenCode and Vibe: a turn ran and ended, and neither a reply
// nor an error reached the chat. The user is told the turn ended without
// one instead of looking at their prompt and nothing else.
func TestTurnWithoutReplySaysSo(t *testing.T) {
	const id = "s_noreply001"
	var status atomic.Value
	status.Store("idle")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
	s, _ := e.served(context.Background(), id)
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "hi", Mode: harness.SendNow}); err != nil {
		t.Fatal(err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	waitFor(t, func() bool { return len(noticesIn(e, id)) == 1 })
	if n := noticesIn(e, id)[0].Notice; n.Code != harness.NoticeError {
		t.Fatalf("notice = %+v", n)
	}

	// A turn with a reply says nothing more.
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "again", Mode: harness.SendNow}); err != nil {
		t.Fatal(err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "true"},
		"items": []harness.Item{{ID: "a1", Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: "ok"}}}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	if n := len(noticesIn(e, id)); n != 1 {
		t.Fatalf("%d notices after a turn with a reply", n)
	}
}

// omp writes its session file after the pane went idle: a reply that
// lands seconds after the turn ended is not "no reply".
func TestLateReplyIsNotMissing(t *testing.T) {
	const id = "s_noreply002"
	var status atomic.Value
	status.Store("idle")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status)
	s, _ := e.served(context.Background(), id)
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "hi", Mode: harness.SendNow}); err != nil {
		t.Fatal(err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	deliver(id, map[string]any{"items": []harness.Item{{ID: "a1", Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: "ok"}}}})
	time.Sleep(time.Second)
	if n := len(noticesIn(e, id)); n != 0 {
		t.Fatalf("%d notices for a reply that came late", n)
	}
}
