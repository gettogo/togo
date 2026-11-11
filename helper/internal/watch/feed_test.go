package watch

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// An agent whose own feed says its turn runs (OpenCode waiting minutes on a
// model that streams nothing) is not done when its pane falls silent: no
// idle, no done, until the feed says the turn ended.
func TestSilenceWhileFeedWorksIsNoDone(t *testing.T) {
	h := newHarness(t)
	var running atomic.Bool
	running.Store(true)
	h.w.SetTurnRunning(func(id string) bool { return id == sid && running.Load() })
	h.work(t)
	for range 10 {
		h.advance(7 * time.Second)
		h.w.Tick(context.Background())
	}
	if len(h.events) != 0 {
		t.Fatalf("events while the feed works = %+v, want none", h.events)
	}
	for _, s := range h.statuses {
		if s == sid+":idle" {
			t.Fatalf("statuses = %v, want no idle while the feed works", h.statuses)
		}
	}
	running.Store(false)
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if len(h.events) != 1 || h.events[0].Level != wire.LevelDone {
		t.Fatalf("events after the turn ended = %+v, want one done", h.events)
	}
	if last := h.statuses[len(h.statuses)-1]; last != sid+":idle" {
		t.Fatalf("statuses = %v, want idle last", h.statuses)
	}
}

// A feed that keeps saying the turn runs while the pane sits silent at the
// prompt (OpenCode's stream dropped as the turn ended; an omp or Pi file
// that never records a turn end) holds the done back for maxVouched at
// most: then the pane decides, and the done goes out.
func TestFeedStuckWorkingStillEndsInDone(t *testing.T) {
	h := newHarness(t)
	h.w.SetTurnRunning(func(id string) bool { return id == sid })
	h.work(t)
	for i := 0; i < int(maxVouched/(7*time.Second))+3; i++ {
		h.advance(7 * time.Second)
		h.w.Tick(context.Background())
	}
	if len(h.events) != 1 || h.events[0].Level != wire.LevelDone {
		t.Fatalf("events = %+v, want one done once the vouch ran out", h.events)
	}
	if last := h.statuses[len(h.statuses)-1]; last != sid+":idle" {
		t.Fatalf("statuses = %v, want idle last", h.statuses)
	}
}

// While the feed vouches for a silent turn of an agent with a working
// indicator, the indicator is read once per idle window, not every two
// seconds with an inspect every four: a five-minute think cost about 200
// captures. (The prompt poll of a repainting pane, every promptPoll, stays.)
func TestVouchedTurnIsReadOncePerIdleWindow(t *testing.T) {
	h := newAgentHarness(t, "pi")
	h.w.SetTurnRunning(func(id string) bool { return id == sid })
	h.setScreen("⠙ Working... (escape to interrupt)\n> \n")
	h.outputFor(t, 4*time.Second)
	h.setScreen("> \n") // indicator gone, the model thinks silently
	h.captures = 0
	for range 60 { // a minute, ticking every second while redraws arrive
		h.signal(t, wire.SignalOutput, "")
		h.advance(time.Second)
		h.w.Tick(context.Background())
	}
	if h.captures > 42 { // 60 before: 30 indicator reads, 15 inspects, 15 polls
		t.Fatalf("%d captures in a vouched minute, want about one per idle window", h.captures)
	}
	if len(h.events) != 0 {
		t.Fatalf("events = %+v, want none while the feed vouches", h.events)
	}
}

// A question or approval waiting on the user (OpenCode reported idle while
// its permission waited, and the phone heard "done") holds off idle and
// done for as long as it waits: no limit turns the wait into a done, the
// feed's vouch cap included. Answered, the silence is a done again.
func TestWaitOnTheUserIsNeverDone(t *testing.T) {
	h := newHarness(t)
	var waits atomic.Bool
	waits.Store(true)
	h.w.SetNeedsYou(func(id string) bool { return id == sid && waits.Load() })
	h.w.SetTurnRunning(func(string) bool { return false })
	h.work(t)
	for range int(2*maxVouched/(7*time.Second)) + 3 {
		h.advance(7 * time.Second)
		h.w.Tick(context.Background())
	}
	if len(h.events) != 0 {
		t.Fatalf("events while the user is needed = %+v, want none", h.events)
	}
	for _, s := range h.statuses {
		if s == sid+":idle" {
			t.Fatalf("statuses = %v, want no idle while the user is needed", h.statuses)
		}
	}
	waits.Store(false)
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if len(h.events) != 1 || h.events[0].Level != wire.LevelDone {
		t.Fatalf("events after the answer = %+v, want one done", h.events)
	}
}
