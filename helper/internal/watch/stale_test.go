package watch

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// statusHarness reads back the last status set, as the hub does.
type statusHarness struct{ *harness }

func (h statusHarness) Status(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.statuses) - 1; i >= 0; i-- {
		if s := h.statuses[i]; len(s) > len(id) && s[:len(id)] == id {
			return s[len(id)+1:]
		}
	}
	return ""
}

func clineScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../harness/adapters/cline/testdata/screens/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// spinFrame turns the capture's spinner (⠴) to frame i, as Cline redraws it.
func spinFrame(screen string, i int) string {
	frames := []string{"⠴", "⠦", "⠧", "⠇", "⠏", "⠋", "⠙", "⠹"}
	return strings.Replace(screen, "⠴", frames[i%len(frames)], 1)
}

// Cline 3.0.74 draws no indicator the watcher knows. Seen live in the
// conformance suite: the turn worked (spinner, sustained output), its
// permission prompt was found while the spinner beside it kept the pane
// busy, the phone approved it, and Cline finished within a second ("*
// done" at its idle input box). That last burst was too short to count as
// work, so nothing settled the session: it read "working" for minutes and
// the next prompt sat in the queue. Output once the prompt is gone goes on
// with the turn the prompt paused, so the silence after it settles the
// session, with the done the turn earned.
func TestStaleWorkingAfterPromptAnsweredSettles(t *testing.T) {
	h := newAgentHarness(t, "cline")
	h.w.cfg.Status = statusHarness{h}
	h.advance(startupGrace)
	ctx := context.Background()

	thinking := clineScreen(t, "real-approval-thinking")
	for i := range 8 { // 4 s of spinner
		h.setScreen(spinFrame(thinking, 2*i))
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.setScreen(spinFrame(thinking, 2*i+1))
		h.w.Tick(ctx)
	}
	if got := (statusHarness{h}).Status(sid); got != statusWorking {
		t.Fatalf("status after the spinner = %q, want working", got)
	}
	// The permission prompt, with the spinner still turning beside it.
	prompt := clineScreen(t, "real-approval-prompt-spinner")
	for i := range 12 {
		h.setScreen(spinFrame(prompt, 2*i))
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.setScreen(spinFrame(prompt, 2*i+1))
		h.w.Tick(ctx)
	}
	if len(h.seen) != 1 {
		t.Fatalf("PromptSeen calls = %d, want 1", len(h.seen))
	}
	// Approved: under a second of thinking, then the reply.
	h.setScreen(clineScreen(t, "real-approval-answered-thinking"))
	h.signal(t, wire.SignalOutput, "")
	h.advance(500 * time.Millisecond)
	h.w.Tick(ctx)
	h.setScreen(clineScreen(t, "real-approval-answered-done"))
	h.signal(t, wire.SignalOutput, "")
	for range 30 {
		h.advance(time.Second)
		h.w.Tick(ctx)
	}
	if got := (statusHarness{h}).Status(sid); got != statusIdle {
		t.Fatalf("status 30 s after the reply = %q, want idle (statuses %v)", got, h.statuses)
	}
	done := 0
	for _, ev := range h.events {
		if ev.Level == wire.LevelDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("done notifications = %d, want 1", done)
	}
}

// A prompt answered while nothing worked (a start-up dialog) leaves no
// turn to go on with: the short redraw after it is no work.
func TestPromptAnsweredWhileIdleIsNoWork(t *testing.T) {
	h := newAgentHarness(t, "cline")
	h.w.cfg.Status = statusHarness{h}
	h.advance(startupGrace)
	ctx := context.Background()
	prompt := clineScreen(t, "real-approval-prompt-spinner")
	for i := range 12 {
		h.setScreen(spinFrame(prompt, 2*i))
		h.signal(t, wire.SignalOutput, "")
		h.advance(500 * time.Millisecond)
		h.setScreen(spinFrame(prompt, 2*i+1))
		h.w.Tick(ctx)
	}
	if len(h.seen) != 1 {
		t.Fatalf("PromptSeen calls = %d, want 1", len(h.seen))
	}
	h.statuses = nil
	h.setScreen(clineScreen(t, "real-approval-answered-done"))
	h.signal(t, wire.SignalOutput, "")
	for range 30 {
		h.advance(time.Second)
		h.w.Tick(ctx)
	}
	if len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("statuses=%v events=%v, want none", h.statuses, h.events)
	}
}
