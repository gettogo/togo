package watch

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Copilot keeps repainting while its start-up question waits (seen live:
// "Would you like to add this key binding…" never reached Needs you), so
// the pane never falls silent. For a coding agent the screen is read for a
// prompt every few seconds while output goes on.
func TestPromptUnderContinuousRedrawIsSeen(t *testing.T) {
	h := newHarness(t)
	h.w.entry(sid).agent = "copilot"
	h.setScreen(promptA)
	for range 40 {
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.w.Tick(context.Background())
	}
	if len(h.seen) != 1 {
		t.Fatalf("PromptSeen calls = %d, want 1", len(h.seen))
	}
}

// With no prompt, the reads stay spaced out.
func TestRedrawPollIsSpaced(t *testing.T) {
	h := newHarness(t)
	h.w.entry(sid).agent = "copilot"
	for range 40 { // 20 s of repainting
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.w.Tick(context.Background())
	}
	if h.captures > 6 {
		t.Fatalf("captures = %d in 20 s: polling too often", h.captures)
	}
}

// A shell's endless output (a log tail) is not polled.
func TestShellOutputIsNotPolled(t *testing.T) {
	h := newHarness(t)
	h.setScreen(promptA)
	for range 40 {
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.w.Tick(context.Background())
	}
	if h.captures != 0 {
		t.Fatalf("captures = %d", h.captures)
	}
}
