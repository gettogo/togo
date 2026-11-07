package watch

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Copilot's folder trust dialog in a short pane shows one option and is no
// prompt yet; the phone attaching resizes the pane and the redraw shows the
// whole dialog. The redraw is no work, but the prompt it reveals is
// reported (e2e group B, Copilot CLI 1.0.107).
func TestPromptRevealedByAResizeIsReported(t *testing.T) {
	h := newHarness(t)
	geom := "80x24/0"
	h.w.geometry = func(context.Context, string) (string, error) { return geom, nil }
	h.setScreen("Copilot\n")
	h.signal(t, wire.SignalOutput, "")
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	h.statuses, h.events = nil, nil

	geom = "100x50/1"
	h.setScreen(promptA)
	h.signal(t, wire.SignalOutput, "")
	h.advance(3 * time.Second)
	h.w.Tick(context.Background())
	if len(h.seen) != 1 {
		t.Fatalf("prompts seen %d, want the dialog the resize revealed", len(h.seen))
	}
	for _, s := range h.statuses {
		if s == sid+":working" {
			t.Fatalf("the redraw counted as work: %v", h.statuses)
		}
	}
}
