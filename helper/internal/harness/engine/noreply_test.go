package engine

import (
	"context"
	"slices"
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

// doneEngine is statusEngine with a done push: the missing-reply check
// runs with it, so settled can wait for the check without a fixed pause.
// The scale makes turnSettle 3 s, well past the typing a test waits for.
func doneEngine(t *testing.T, id string, pane harnessTypist) (*Engine, *sess, func()) {
	t.Helper()
	var status atomic.Value
	status.Store("idle")
	done := make(chan struct{}, 8)
	e := statusEngine(t, id, pane, &status, func(c *Config) {
		c.Scale = 0.5
		c.Done = func(string, string, string) { done <- struct{}{} }
	})
	s, _ := e.served(context.Background(), id)
	// settled waits for a turn's done push; the missing-reply check runs
	// under the same hold of the session lock, so it has run once the lock
	// is free again. The fake adapter's hook feed would leave the push to a
	// Stop hook; forgetting its records makes the engine send it.
	settled := func() {
		t.Helper()
		s.mu.Lock()
		delete(s.last, "events")
		s.mu.Unlock()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("no done push")
		}
		s.mu.Lock()
		s.mu.Unlock() //nolint:staticcheck // waits for the check to finish
	}
	return e, s, settled
}

// A prompt queued while the agent works is typed as that turn ends. The
// missing-reply check armed by that turn's end belongs to the prompt the
// turn answered, not to the queued one: the queued prompt is judged only
// once its own turn ended. Found by the end-to-end suite on omp, where the
// queued prompt got "ended the turn without a reply" before its turn ran.
func TestQueuedPromptJudgedAfterItsOwnTurn(t *testing.T) {
	const id = "s_noreply003"
	pane := &seqPane{mode: "a"}
	e, s, settled := doneEngine(t, id, pane)
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "first", Mode: harness.SendNow}); err != nil {
		t.Fatal(err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "true"},
		"items": []harness.Item{{ID: "a1", Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: "ok"}}}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	queue(t, e, id, "queued")
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { _, typed := pane.snapshot(); return slices.Contains(typed, "queued") })
	settled()
	if n := noticesIn(e, id); len(n) != 0 {
		t.Fatalf("the queued prompt was judged before its turn ran: %+v", n)
	}

	// Its own turn ends with nothing said: now it is judged.
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	settled()
	if n := noticesIn(e, id); len(n) != 1 || n[0].Notice.Code != harness.NoticeError {
		t.Fatalf("notices after the queued prompt's turn = %+v", n)
	}
}

// A turn that fails at once (a model server that is down) can start and
// end before sending the prompt returned. That turn was the prompt's own:
// it is judged, though the prompt was marked sent after the turn ended.
func TestPromptWhoseTurnEndedWhileSendingIsJudged(t *testing.T) {
	const id = "s_noreply004"
	pane := &blockPane{seqPane: seqPane{mode: "a"}, started: make(chan string, 4), release: make(chan struct{})}
	e, s, settled := doneEngine(t, id, pane)
	sent := make(chan error, 1)
	go func() {
		_, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "hi", Mode: harness.SendNow})
		sent <- err
	}()
	<-pane.started
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitFor(t, func() bool { return s.sum.Load().Working })
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { return !s.sum.Load().Working })
	close(pane.release)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	settled()
	if n := noticesIn(e, id); len(n) != 1 || n[0].Notice.Code != harness.NoticeError {
		t.Fatalf("notices = %+v", n)
	}
}
