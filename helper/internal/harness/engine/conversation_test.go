package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// waitSendBlockedOnTypingLock waits until a goroutine inside Send is parked
// on the typing lock. Send reads the conversation before it waits for the
// lock, so by then it has seen the conversation the test means it to see.
func waitSendBlockedOnTypingLock(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		var b strings.Builder
		if err := pprof.Lookup("goroutine").WriteTo(&b, 2); err != nil {
			t.Fatal(err)
		}
		for _, g := range strings.Split(b.String(), "\n\n") {
			if strings.Contains(g, "(*Engine).Send(") && strings.Contains(g, "sync.Mutex.Lock") {
				return
			}
		}
	}
	t.Fatal("the send never reached the typing lock")
}

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

func init() {
	harness.RegisterAdapterFile("fakegate", []byte(`
adapter: fakegate
name: Gated
revision: 1
match: { agents: [fakegate] }
capabilities: [chat, send.api, send.queue, interrupt]
feeds:
  events:
    hook: { events: [Event] }
    mapper: fake.events
    stability: documented
actions:
  send: { cli: { argv: [gate, '{{.text}}'] }, stability: documented }
`))
}

// A prompt is on its way through the harness API when the agent moves to
// another conversation. Its request returns into a chat that has started
// over: the prompt is not shown as sent there and is not sent again, for a
// direct send and for a queued one.
func TestSendInFlightAcrossAConversationMove(t *testing.T) {
	const id = "s_convgate01"
	bin, home := t.TempDir(), t.TempDir()
	// The request waits until its release file exists.
	script := "#!/bin/sh\necho \"$1\" >> sent.log\nwhile [ ! -e release ]; do sleep 0.05; done\n"
	if err := os.WriteFile(filepath.Join(bin, "gate"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var status atomic.Value
	status.Store("idle")
	var epochs atomic.Int64
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakegate", Managed: true, Cwd: home, Command: []string{"fakegate"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		Status:    func(string) string { v, _ := status.Load().(string); return v },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e" + strconv.FormatInt(epochs.Add(1), 10) }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	sentLog := func() string { b, _ := os.ReadFile(filepath.Join(home, "sent.log")); return string(b) }
	chat := func() (users []string, notices string) {
		res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range res.Items {
			if it.User != nil {
				users = append(users, it.User.Text)
			}
			if it.Notice != nil {
				notices += it.Notice.Text + "\n"
			}
		}
		return users, notices
	}
	// moveTo reports that the agent moved to conv and waits for the chat to
	// start over: a new log epoch. The epoch is read first so the log
	// exists; a move before anything made the log would name its first
	// epoch, the same name a counter gives the log before a move.
	moveTo := func(conv string) {
		before := e.log.Epoch(id)
		deliver(id, map[string]any{"state": map[string]string{"conversation": conv}})
		waitFor(t, func() bool { return e.log.Epoch(id) != before })
	}
	deliver(id, map[string]any{"state": map[string]string{"conversation": "A"}})
	waitFor(t, func() bool { s, _ := e.served(ctx, id); s.mu.Lock(); defer s.mu.Unlock(); return s.conv == "A" })

	// A direct send.
	done := make(chan error, 1)
	go func() {
		_, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "direct", Mode: harness.SendNow})
		done <- err
	}()
	waitFor(t, func() bool { return strings.Contains(sentLog(), "direct") })
	moveTo("B")
	if err := os.WriteFile(filepath.Join(home, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var moved *harness.FallbackError
	if err := <-done; !errors.As(err, &moved) || !strings.Contains(moved.Detail, "moved to another conversation") {
		t.Fatalf("direct send across the move = %v, want the moved error", err)
	}
	if users, _ := chat(); len(users) != 0 {
		t.Errorf("new chat shows %q as sent", users)
	}

	// A send still waiting for the typing lock when the agent moves is not
	// sent at all.
	os.Remove(filepath.Join(home, "release"))
	l := typingLock(id)
	l.Lock()
	go func() {
		_, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "waiting", Mode: harness.SendNow})
		done <- err
	}()
	waitSendBlockedOnTypingLock(t)
	moveTo("B2")
	l.Unlock()
	if err := <-done; !errors.As(err, &moved) || !strings.Contains(moved.Detail, "was not sent") {
		t.Fatalf("send that waited across the move = %v, want the not-sent error", err)
	}
	if strings.Contains(sentLog(), "waiting") {
		t.Error("a prompt written for the old conversation was sent to the new one")
	}

	// A queued send that is on its way.
	os.Remove(filepath.Join(home, "release"))
	status.Store("working")
	queue(t, e, id, "queued")
	status.Store("idle")
	waitFor(t, func() bool { return strings.Contains(sentLog(), "queued") })
	moveTo("C")
	if err := os.WriteFile(filepath.Join(home, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, notices := chat(); return strings.Contains(notices, "queued") })
	users, notices := chat()
	if len(users) != 0 || !strings.Contains(notices, "may not have arrived") {
		t.Errorf("after the queued send: users %q, notices %q", users, notices)
	}
	time.Sleep(400 * time.Millisecond)
	if n := strings.Count(sentLog(), "queued"); n != 1 {
		t.Errorf("queued prompt sent %d times, want 1", n)
	}
}
