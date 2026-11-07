package watch

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	claudeIdleScreen = "╭────╮\n│ >  │\n╰────╯\n  ? for shortcuts\n"
	claudeBusyScreen = "✻ Pondering… (3s · esc to interrupt)\n╭────╮\n│ >  │\n╰────╯\n"
)

func newAgentHarness(t *testing.T, agent string) *harness {
	t.Helper()
	h := newHarness(t)
	h.sessions[0].Agent = agent
	h.w.Reconcile(context.Background())
	h.w.Tick(context.Background()) // the attach check
	h.statuses = nil
	return h
}

// outputFor sends one output signal a second for d, ticking between them,
// the way status-line redraws arrive.
func (h *harness) outputFor(t *testing.T, d time.Duration) {
	t.Helper()
	for range int(d / time.Second) {
		h.signal(t, wire.SignalOutput, "")
		h.advance(time.Second)
		h.w.Tick(context.Background())
	}
}

// Claude drawing its banner and redrawing its status line at an idle input
// box is not working, and its silence afterwards is not a done.
func TestClaudeStartupAndRedrawsAreNotWork(t *testing.T) {
	h := newAgentHarness(t, "claude")
	h.setScreen(claudeIdleScreen)
	h.outputFor(t, 30*time.Second)
	h.advance(time.Minute)
	h.w.Tick(context.Background())
	if len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("statuses=%v events=%v, want none", h.statuses, h.events)
	}
}

// Claude is working while its indicator shows, and done once it is gone
// even though the status line keeps redrawing.
func TestClaudeIndicatorDrivesWorking(t *testing.T) {
	h := newAgentHarness(t, "claude")
	h.setScreen(claudeBusyScreen)
	h.outputFor(t, 10*time.Second)
	if len(h.statuses) != 1 || h.statuses[0] != sid+":working" {
		t.Fatalf("statuses = %v, want working", h.statuses)
	}
	h.setScreen(claudeIdleScreen)
	h.outputFor(t, 10*time.Second)
	if len(h.statuses) != 2 || h.statuses[1] != sid+":idle" {
		t.Fatalf("statuses = %v, want working then idle", h.statuses)
	}
	if len(h.events) != 1 || h.events[0].Level != wire.LevelDone {
		t.Fatalf("events = %+v, want one done", h.events)
	}
}

// A permission prompt Claude draws without a working turn before it (the
// folder trust question at start) is still reported.
func TestClaudePromptWithoutWorkIsReported(t *testing.T) {
	h := newAgentHarness(t, "claude")
	h.setScreen(promptA)
	h.outputFor(t, 2*time.Second)
	h.advance(7 * time.Second)
	h.w.Tick(context.Background())
	if len(h.seen) != 1 {
		t.Fatalf("prompts seen %d, want 1", len(h.seen))
	}
	if len(h.statuses) != 0 {
		t.Fatalf("statuses = %v", h.statuses)
	}
}

// Output right after the pane is resized or a client attaches is the
// program repainting: no status and no activity.
func TestRedrawAfterResizeIsIgnored(t *testing.T) {
	h := newHarness(t)
	rec := &activityRecorder{}
	h.w.cfg.Activity = rec
	geom := "80x24/0"
	h.w.geometry = func(context.Context, string) (string, error) { return geom, nil }
	h.signal(t, wire.SignalOutput, "")
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	rec.ids, h.statuses, h.events = nil, nil, nil

	geom = "120x40/1"
	h.signal(t, wire.SignalOutput, "")
	h.advance(time.Second)
	h.signal(t, wire.SignalOutput, "")
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if len(rec.ids) != 0 || len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("activity=%v statuses=%v events=%v, want none", rec.ids, h.statuses, h.events)
	}
	// Output once the window has passed counts again.
	h.work(t)
	if len(rec.ids) != 3 || len(h.statuses) != 1 || h.statuses[0] != sid+":working" {
		t.Fatalf("activity=%v statuses=%v", rec.ids, h.statuses)
	}
}

// A program without an indicator needs sustained output, not one burst,
// and its start-up output does not count.
func TestPlainOutputNeedsSustainedOutputAfterStartup(t *testing.T) {
	h := newAgentHarness(t, "")
	h.outputFor(t, 4*time.Second) // start-up banner
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	h.signal(t, wire.SignalOutput, "") // a lone redraw
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if len(h.statuses) != 0 || len(h.events) != 0 {
		t.Fatalf("statuses=%v events=%v, want none", h.statuses, h.events)
	}
	h.work(t)
	if len(h.statuses) != 1 || h.statuses[0] != sid+":working" {
		t.Fatalf("statuses = %v, want working", h.statuses)
	}
}

// readBack is a status sink that can be read back, as the hub can; it
// starts with the status hooks set.
type readBack struct {
	*harness
	status string
}

func (r *readBack) SetStatus(id, status string, a *string) {
	r.status = status
	r.harness.SetStatus(id, status, a)
}

func (r *readBack) Status(string) string { return r.status }

const claudePermissionScreen = "Bash command\n  npm test\nDo you want to proceed?\n❯ 1. Yes\n  2. No, and tell Claude what to do differently (esc)\n"

// The owner interrupts Claude at a permission prompt. The prompt had
// already settled the turn without changing the status hooks set to
// working, and no Stop hook follows an interrupt. Once the prompt is gone
// and no working indicator shows, the session is idle, with no done
// notification: an interrupt is not a finished turn.
func TestClaudeInterruptAtPromptEndsWorking(t *testing.T) {
	h := newAgentHarness(t, "claude")
	sink := &readBack{harness: h}
	h.w.cfg.Status = sink
	h.setScreen(claudeBusyScreen)
	h.outputFor(t, 4*time.Second)
	h.setScreen(claudePermissionScreen)
	h.outputFor(t, 6*time.Second)
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if len(h.seen) != 1 {
		t.Fatalf("prompts seen %d, want the permission prompt", len(h.seen))
	}
	if sink.status != statusWorking {
		t.Fatalf("status = %q; a pending prompt leaves the hooks' working alone", sink.status)
	}
	h.events = nil

	h.setScreen(claudeIdleScreen) // Esc: the prompt closes, the turn is over
	h.outputFor(t, time.Second)
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if sink.status != statusIdle {
		t.Fatalf("status = %q after the interrupt, want idle (statuses %v)", sink.status, h.statuses)
	}
	if len(h.events) != 0 {
		t.Fatalf("events = %+v, want no done notification", h.events)
	}
}

// Typing at Claude's idle input box, with the status already idle, sets
// nothing.
func TestClaudeIdleOutputKeepsStatus(t *testing.T) {
	h := newAgentHarness(t, "claude")
	sink := &readBack{harness: h, status: statusIdle}
	h.w.cfg.Status = sink
	h.setScreen(claudeIdleScreen)
	h.outputFor(t, 3*time.Second)
	h.advance(10 * time.Second)
	h.w.Tick(context.Background())
	if len(h.statuses) != 0 {
		t.Fatalf("statuses = %v, want none", h.statuses)
	}
}
