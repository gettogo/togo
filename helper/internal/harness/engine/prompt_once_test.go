package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// recordPane is an agent that writes its own record of a submitted prompt
// to the feed. With early set, the record is written as Enter is pressed and
// the key press is held until the engine has read it: the order a slow
// machine produces, where the agent's file is read before typing returns.
// fail makes the key press fail without the agent seeing anything.
type recordPane struct {
	fakePane
	e     *Engine
	tb    *testing.T
	done  chan struct{}  // closed when the test ends
	busy  sync.WaitGroup // key presses still running
	mu    sync.Mutex
	early bool
	fail  bool
	n     int
}

func (p *recordPane) Type(ctx context.Context, id, text string, submit, paste bool) error {
	p.busy.Add(1)
	defer p.busy.Done()
	p.mu.Lock()
	early, fail := p.early, p.fail
	p.mu.Unlock()
	if fail && submit {
		return errors.New("the pane went away")
	}
	if err := p.fakePane.Type(ctx, id, text, submit, paste); err != nil {
		return err
	}
	if early && submit {
		rec := p.record(id, text)
		// Typing may run on the queue's goroutine, so this waits without
		// t.Fatal, and gives up once the test has ended.
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(5 * time.Millisecond) {
			if _, ok := p.e.log.Get(id, rec); ok || p.hasRenamed(id, rec) {
				break
			}
			select {
			case <-p.done:
				return nil
			default:
			}
			if time.Now().After(deadline) {
				p.tb.Errorf("the engine never read the agent's record of %q", text)
				return nil
			}
		}
	}
	return nil
}

// record delivers the agent's record of text and returns its id.
func (p *recordPane) record(id, text string) string {
	p.mu.Lock()
	p.n++
	rec := fmt.Sprintf("agent-%d", p.n)
	p.mu.Unlock()
	deliver(id, map[string]any{"items": []harness.Item{{ID: rec, TurnID: rec, Kind: harness.KindUser, User: &harness.UserBody{Text: text}}}})
	return rec
}

// hasRenamed reports whether the engine took the record rec as a phone
// prompt (it then carries the phone item's id).
func (p *recordPane) hasRenamed(id, rec string) bool {
	for _, it := range allItems(p.e, id) {
		if it.TurnID == rec {
			return true
		}
	}
	return false
}

func newRecordEngine(t *testing.T, id string) (*Engine, *recordPane) {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	home := t.TempDir()
	pane := &recordPane{fakePane: fakePane{mode: "a"}, tb: t, done: make(chan struct{})}
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fake", Managed: true, Cwd: home}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.2,
	})
	pane.e = e
	t.Cleanup(func() {
		close(pane.done)
		pane.busy.Wait()
		e.Forget(id)
	})
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e, pane
}

// waitUntil waits for cond, at most 30 seconds.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 30s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func allItems(e *Engine, id string) []harness.Item {
	items, _, _ := e.log.Since(id, 0, MaxItems)
	return items
}

// prompts are the chat's prompts with this text.
func prompts(e *Engine, id, text string) []harness.Item {
	var out []harness.Item
	for _, it := range allItems(e, id) {
		if it.Kind == harness.KindUser && it.User != nil && it.User.Text == text && !it.User.Withdrawn {
			out = append(out, it)
		}
	}
	return out
}

func sendNow(t *testing.T, e *Engine, id, text string) harness.SendResult {
	t.Helper()
	r, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: text, Mode: harness.SendNow})
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	return r
}

// A prompt the agent records before typing returns shows once, as the
// phone's prompt.
func TestPromptRecordedBeforeSendReturnsShowsOnce(t *testing.T) {
	const id = "s_promptonce1"
	e, pane := newRecordEngine(t, id)
	pane.early = true
	const text = "Carry on, recorded before the send returns"
	r := sendNow(t, e, id, text)
	got := prompts(e, id, text)
	if len(got) != 1 || got[0].ID != r.Item.ID || got[0].User.Source != "phone" || got[0].TurnID != "agent-1" {
		t.Fatalf("prompts = %+v, sent %+v", got, r.Item)
	}
	// The record read again (its conversation's past loaded) stays that item.
	deliver(id, map[string]any{"items": []harness.Item{{ID: "agent-1", TurnID: "agent-1", Kind: harness.KindUser, User: &harness.UserBody{Text: text}}}})
	deliver(id, map[string]any{"items": []harness.Item{{ID: "a-reply", Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: "ok"}}}})
	waitUntil(t, func() bool { _, ok := e.log.Get(id, "a-reply"); return ok })
	if got := prompts(e, id, text); len(got) != 1 {
		t.Fatalf("after replay prompts = %+v", got)
	}
}

// A prompt the agent records after typing returns shows once.
func TestPromptRecordedAfterSendReturnsShowsOnce(t *testing.T) {
	const id = "s_promptonce2"
	e, pane := newRecordEngine(t, id)
	const text = "Recorded after the send returns"
	r := sendNow(t, e, id, text)
	pane.record(id, text)
	waitUntil(t, func() bool { it, _ := e.log.Get(id, r.Item.ID); return it.TurnID == "agent-1" })
	if got := prompts(e, id, text); len(got) != 1 || got[0].User.Source != "phone" {
		t.Fatalf("prompts = %+v", got)
	}
}

// A prompt whose typing failed leaves nothing in the chat, and the agent
// later recording the same words (typed at the desk) is not taken for it.
func TestPromptTypingFailureLeavesNoPhantom(t *testing.T) {
	const id = "s_promptonce3"
	e, pane := newRecordEngine(t, id)
	pane.fail = true
	const text = "This one never reaches the agent"
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: text, Mode: harness.SendNow}); err == nil {
		t.Fatal("a failed typing was reported as sent")
	}
	if got := prompts(e, id, text); len(got) != 0 {
		t.Fatalf("prompts after a failure = %+v", got)
	}
	pane.record(id, text)
	waitUntil(t, func() bool { _, ok := e.log.Get(id, "agent-1"); return ok })
	if got := prompts(e, id, text); len(got) != 1 || got[0].User.Source == "phone" {
		t.Fatalf("desk prompt = %+v", got)
	}
}

// Two prompts with the same words are two prompts, in either order.
func TestTwoIdenticalPromptsShowTwice(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(fmt.Sprintf("early=%v", early), func(t *testing.T) {
			id := "s_promptsame0"
			if early {
				id = "s_promptsame1"
			}
			e, pane := newRecordEngine(t, id)
			pane.early = early
			const text = "yes"
			first := sendNow(t, e, id, text)
			second := sendNow(t, e, id, text)
			if first.Item.ID == second.Item.ID {
				t.Fatalf("both sends returned %s", first.Item.ID)
			}
			if !early {
				pane.record(id, text)
				pane.record(id, text)
			}
			waitUntil(t, func() bool {
				a, _ := e.log.Get(id, first.Item.ID)
				b, _ := e.log.Get(id, second.Item.ID)
				return a.TurnID == "agent-1" && b.TurnID == "agent-2"
			})
			if got := prompts(e, id, text); len(got) != 2 {
				t.Fatalf("prompts = %+v", got)
			}
		})
	}
}

// A queued prompt the agent records before its typing returns shows once,
// in the queued item's place.
func TestQueuedPromptRecordedBeforeTypingReturnsShowsOnce(t *testing.T) {
	const id = "s_promptonce4"
	e, pane := newRecordEngine(t, id)
	pane.early = true
	deliver(id, map[string]any{"state": map[string]string{"working": "true"}})
	waitUntil(t, func() bool { s, _ := e.Summary(id); return s.Working })
	const text = "Queued, recorded before typing returns"
	r, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: text, Mode: harness.SendQueue})
	if err != nil || !r.Item.User.Queued {
		t.Fatalf("queue = %+v %v", r, err)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitUntil(t, func() bool { it, _ := e.log.Get(id, r.Item.ID); return it.TurnID == "agent-1" && !it.User.Queued })
	if got := prompts(e, id, text); len(got) != 1 || got[0].User.Source != "phone" {
		t.Fatalf("prompts = %+v", got)
	}
}

// A multi-line prompt over MaxItemText matches the agent's record, which
// the chat clips, in either order.
func TestLongPromptShowsOnce(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(fmt.Sprintf("early=%v", early), func(t *testing.T) {
			id := "s_promptlong0"
			if early {
				id = "s_promptlong1"
			}
			e, pane := newRecordEngine(t, id)
			pane.early = early
			text := strings.Repeat("line of a long prompt\n", harness.MaxItemText/20)
			r := sendNow(t, e, id, text)
			if !early {
				pane.record(id, text)
			}
			waitUntil(t, func() bool { it, _ := e.log.Get(id, r.Item.ID); return it.TurnID == "agent-1" })
			n := 0
			for _, it := range allItems(e, id) {
				if it.Kind == harness.KindUser {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%d prompts shown", n)
			}
		})
	}
}
