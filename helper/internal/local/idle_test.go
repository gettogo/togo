package local

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func notifies(pub *fakePub) []wire.NotifyEvent {
	var out []wire.NotifyEvent
	for _, e := range pub.events {
		if n, ok := e.payload.(wire.NotifyEvent); ok {
			out = append(out, n)
		}
	}
	return out
}

// Claude's idle_prompt ("Claude is waiting for your input") fires about a
// minute after a turn ends. It is not a request: no needs_you, and no
// second done after the Stop that already reported the turn.
func TestIdleAfterStopIsSilent(t *testing.T) {
	h, hub, _, pub := setup()
	for _, ev := range []string{EventPrompt, EventStop} {
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{})); err != nil {
			t.Fatal(err)
		}
	}
	before := len(notifies(pub))
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventIdle, wire.HookData{Message: "Claude is waiting for your input"})); err != nil {
		t.Fatal(err)
	}
	if got := notifies(pub); len(got) != before {
		t.Fatalf("idle after stop notified %+v", got[before:])
	}
	if hub.status != statusIdle {
		t.Fatalf("status %q, want idle", hub.status)
	}
}

// A turn that never reported Stop (interrupted, or the hook was lost) is
// reported once as done, never as needs_you.
func TestIdleWithoutStopIsQuietDone(t *testing.T) {
	h, _, _, pub := setup()
	for _, ev := range []string{EventPrompt, EventIdle, EventIdle} {
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{Message: "Claude is waiting for your input"})); err != nil {
			t.Fatal(err)
		}
	}
	got := notifies(pub)
	if len(got) != 1 || got[0].Level != wire.LevelDone {
		t.Fatalf("notifies %+v, want one done", got)
	}
}

// An idle session with no turn seen (a fresh daemon) says nothing.
func TestIdleWithNoTurnSeenIsSilent(t *testing.T) {
	h, _, _, pub := setup()
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventIdle, wire.HookData{})); err != nil {
		t.Fatal(err)
	}
	if got := notifies(pub); len(got) != 0 {
		t.Fatalf("notifies %+v", got)
	}
}

// A permission prompt left pending is moot once the agent sits idle at its
// input or the user starts the next turn: both close it.
func TestIdleAndNextPromptCloseStalePermission(t *testing.T) {
	for _, ev := range []string{EventIdle, EventPrompt} {
		h, _, ap, _ := setup()
		ap.pending = []wire.Approval{{RequestID: "r_bbbbbbbbbb", Source: wire.SourceHook}, {RequestID: "r_cccccccccc", Source: wire.SourceAsk}}
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{})); err != nil {
			t.Fatal(err)
		}
		if len(ap.closed) != 1 || ap.closed[0] != "r_bbbbbbbbbb" {
			t.Fatalf("%s closed %v, want the hook approval", ev, ap.closed)
		}
	}
}

// Idle does not clear an exit: Claude reports an idle prompt while it sits
// stopped on a usage limit.
func TestIdleKeepsExit(t *testing.T) {
	if clearsExit(EventIdle) {
		t.Fatal("idle clears the exit")
	}
}
