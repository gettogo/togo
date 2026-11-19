package engine

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A command answered with nothing to show is still watched. When its list
// opens later, the session counts as holding a list left open and the chat
// that ran the command is told once, by its request id. A command run
// without a request id is watched the same way and nobody is told.
func TestAListThatOpensLateIsHeldAndAnnouncedOnce(t *testing.T) {
	list, err := os.ReadFile("../termmenu/testdata/oc-models.ansi")
	if err != nil {
		t.Fatal(err)
	}
	for _, requestID := range []string{"11111111-2222-3333-4444-555555555555", ""} {
		const id = "s_fakefake21"
		e, _ := newFake(t, id, "1.2.3")
		var open atomic.Bool
		e.cfg.StyledScreen = func(context.Context, string) (string, error) {
			if open.Load() {
				return string(list), nil
			}
			return "", nil
		}
		ctx := context.Background()
		s, err := e.served(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		sub := &fakeSub{done: make(chan struct{})}
		held := func() bool { _, ok := e.menuLeftOpen(s); return ok }
		if _, err := e.Open(ctx, sub, id, 0, 50); err != nil {
			t.Fatal(err)
		}
		e.Release(sub, id)
		menus := func() (n int, request string) {
			sub.mu.Lock()
			defer sub.mu.Unlock()
			for _, note := range sub.got {
				if note.Kind == harness.NoteMenu {
					n, request = n+1, note.RequestID
				}
			}
			return n, request
		}
		call := withMenuConv(ctx, s)
		if requestID != "" {
			call = context.WithValue(call, requestIDKey{}, requestID)
		}
		e.watchLateList(call, s)
		time.Sleep(400 * time.Millisecond)
		if n, _ := menus(); n != 0 || held() {
			t.Fatalf("a list was announced before one opened")
		}
		open.Store(true)
		deadline := time.Now().Add(30 * time.Second)
		for !held() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !held() {
			t.Fatalf("the late list is not held as one left open")
		}
		if requestID != "" {
			// The announcement follows the list being held; wait for it
			// before holding to see that it is not repeated.
			for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				if n, _ := menus(); n > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the late list was never announced")
				}
			}
		}
		time.Sleep(600 * time.Millisecond)
		n, request := menus()
		switch {
		case requestID == "" && n != 0:
			t.Errorf("a command without a request id was announced %d times", n)
		case requestID != "" && (n != 1 || request != requestID):
			t.Errorf("announced %d times for request %q, want once for %q", n, request, requestID)
		}
		e.stopLateWatch(id)
	}
}

// The watch ends with the conversation it was started in: a list on the
// screen afterwards is not this command's, and nobody is told.
func TestTheWatchForALateListEndsWithItsConversation(t *testing.T) {
	list, err := os.ReadFile("../termmenu/testdata/oc-models.ansi")
	if err != nil {
		t.Fatal(err)
	}
	const id = "s_fakefake22"
	e, _ := newFake(t, id, "1.2.3")
	var open atomic.Bool
	e.cfg.StyledScreen = func(context.Context, string) (string, error) {
		if open.Load() {
			return string(list), nil
		}
		return "", nil
	}
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	e.watchLateList(context.WithValue(withMenuConv(ctx, s), requestIDKey{}, "r1"), s)
	s.mu.Lock()
	s.convGen++
	s.mu.Unlock()
	open.Store(true)
	time.Sleep(800 * time.Millisecond)
	if _, ok := e.menuLeftOpen(s); ok {
		t.Error("a list from another conversation was held for the old command")
	}
}
