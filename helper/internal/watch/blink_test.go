package watch

import (
	"context"
	"fmt"
	"testing"
	"time"
)

const vibeIdleScreen = " Mistral Vibe v3.1.0\n──── accept edits ─\n>\n────\n~/code/api        ctx   0%\n"

// Vibe draws its own blinking cursor at the idle input box: a reverse-video
// cell toggled twice a second, so the pane never falls silent. Output that
// leaves the screen's text as it was is no work (seen live: every send was
// refused as busy).
func TestAgentCursorBlinkIsNotWork(t *testing.T) {
	h := newAgentHarness(t, "vibe")
	h.setScreen(vibeIdleScreen)
	h.outputFor(t, 30*time.Second)
	h.advance(time.Minute)
	h.w.Tick(context.Background())
	if len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("statuses=%v events=%v, want none", h.statuses, h.events)
	}
}

// Output that changes the screen still is work, and once only the cursor
// blinks the turn settles even though output goes on.
func TestAgentWorkSettlesWhileItsCursorBlinks(t *testing.T) {
	h := newAgentHarness(t, "vibe")
	h.setScreen(vibeIdleScreen)
	h.outputFor(t, 6*time.Second) // start-up
	for i := range 15 {           // a turn; the screen is read again within textIdlePoll
		h.setScreen(vibeIdleScreen + fmt.Sprintf("Generating %d\n", i))
		h.outputFor(t, time.Second)
	}
	if len(h.statuses) != 1 || h.statuses[0] != sid+":working" {
		t.Fatalf("statuses = %v, want working", h.statuses)
	}
	h.setScreen(vibeIdleScreen + "Done.\n")
	h.outputFor(t, 20*time.Second)
	if len(h.statuses) != 2 || h.statuses[1] != sid+":idle" {
		t.Fatalf("statuses = %v, want working then idle", h.statuses)
	}
}
