package watch

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// pendingHarness is a sink that also reports open approvals.
type pendingHarness struct {
	*harness
	pending []wire.Approval
}

func (p *pendingHarness) Pending(string) []wire.Approval { return p.pending }

// Aider at 47 columns, as the owner saw it: the chat prompt landed in
// the OpenRouter question, Aider refused it and asked again. Once the
// first request was answered, the re-asked question is a new request.
func TestReaskedPromptOpensNewRequest(t *testing.T) {
	h := newHarness(t)
	ph := &pendingHarness{harness: h, pending: []wire.Approval{{RequestID: "r1"}}}
	h.w.cfg.Prompts = ph
	h.w.entry(sid).agent = "aider"
	first := "OpenRouter provides free and paid access to\nmany LLMs.\nLogin to OpenRouter or create a free account? (\nY)es/(N)o [Yes]: \n"
	h.setScreen(first)
	h.work(t)
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if len(h.seen) != 1 {
		t.Fatalf("first prompt: PromptSeen calls = %d, want 1", len(h.seen))
	}
	// Same screen redrawn while the request is open: no new request.
	h.signal(t, wire.SignalOutput, "")
	if len(h.seen) != 1 {
		t.Fatalf("redraw: PromptSeen calls = %d, want 1", len(h.seen))
	}
	// The request was answered (by a typed line), Aider refused and re-asked.
	ph.pending = nil
	b, err := os.ReadFile("../detect/testdata/aider/real-openrouter-reask-47col.txt")
	if err != nil {
		t.Fatal(err)
	}
	h.setScreen(string(b))
	h.advance(3 * time.Second)
	h.signal(t, wire.SignalOutput, "")
	if len(h.seen) != 2 {
		t.Fatalf("re-ask: PromptSeen calls = %d, want 2", len(h.seen))
	}
	// Further redraws of the re-asked prompt, now pending again, stay one request.
	ph.pending = []wire.Approval{{RequestID: "r2"}}
	h.signal(t, wire.SignalOutput, "")
	if len(h.seen) != 2 {
		t.Fatalf("after re-ask: PromptSeen calls = %d, want 2", len(h.seen))
	}
}
