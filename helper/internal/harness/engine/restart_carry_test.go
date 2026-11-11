package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// A relaunch that gives the agent a new endpoint restarts the session's
// state; an open chat used to be closed with nothing sent, so the phone
// (and the suite) saw no more items. Its subscribers now carry over: they
// get a reset (reopen for a fresh mission) and the items that follow.
func TestOpenChatSurvivesARestart(t *testing.T) {
	const id = "s_fakecarry1"
	home := t.TempDir()
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakestart", Managed: true, Cwd: home, Command: []string{"fakestart"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return os.Getenv("PATH") },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	sub := &fakeSub{done: make(chan struct{})}
	ctx := context.Background()
	if _, err := e.Open(ctx, sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	e.Release(sub, id)
	e.restart(id)
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.mu.Lock()
		_, rebuilt := e.sessions[id]
		e.mu.Unlock()
		if rebuilt || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s.waitReady(ctx)
	deliver(id, map[string]any{"items": []harness.Item{{ID: "a1", Kind: harness.KindAssistant, At: time.Now(), Assistant: &harness.TextBody{Text: "ok"}}}})
	var reset, item bool
	for i := 0; i < 200 && !(reset && item); i++ {
		sub.mu.Lock()
		for _, n := range sub.got {
			reset = reset || n.Kind == harness.NoteReset
			for _, it := range n.Items {
				item = item || it.ID == "a1"
			}
		}
		sub.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !reset || !item {
		t.Fatalf("after the restart: reset %v, item %v", reset, item)
	}
}
