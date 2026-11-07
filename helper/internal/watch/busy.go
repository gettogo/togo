package watch

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// How output becomes "working".
//
// Output alone is a poor sign of work: an agent paints a banner when it
// starts, repaints its whole screen when a client attaches or resizes the
// window, and redraws its status line while it waits at the input box.
// So:
//   - output in the redrawGrace after the pane's size or client count
//     changed is ignored outright, for status and the activity bars alike;
//   - an agent that draws a working indicator (detect.KnowsBusy) is
//     working only while the screen shows it, and its turn is over once
//     the indicator is gone, even if redraws continue;
//   - any other program is working only after startupGrace has passed
//     since the tap attached and output has run for sustainedOutput.
const (
	redrawGrace     = 2 * time.Second
	startupGrace    = 5 * time.Second
	sustainedOutput = 2 * time.Second
	// burstGap is the longest pause inside one run of output. The tap
	// reports output at most once a second.
	burstGap = 2500 * time.Millisecond
	// busyCheckEvery spaces the screen reads that look for the working
	// indicator while a session works; busyMissesToSettle reads in a row
	// without it end the turn.
	busyCheckEvery     = 2 * time.Second
	busyMissesToSettle = 2
)

func (w *Watcher) output(ctx context.Context, id string, e *entry) {
	if w.Ended(id) {
		return
	}
	now := w.now()
	if w.selfRedraw(ctx, id, e, now) {
		if e.prompt == "" {
			// A dialog the old size cut off (Copilot's folder trust
			// question in a short pane) can be whole after the resize:
			// read the screen once the redraw is drawn, without counting
			// the redraw as work.
			e.promptCheck = true
		}
		return
	}
	if w.cfg.Activity != nil {
		w.cfg.Activity.RecordOutput(id)
	}
	if e.prompt != "" {
		p, ok, err := w.inspect(ctx, id)
		if err != nil {
			e.lastOutput, e.awaitingIdle = now, true
			return
		}
		if ok && p.Fingerprint == e.prompt {
			return
		}
		w.clearPrompt(ctx, id, e)
	}
	if detect.KnowsBusy(e.agent) {
		w.agentOutput(ctx, id, e, now)
		return
	}
	w.plainOutput(id, e, now)
}

// selfRedraw reports whether output at now falls in the redraw window
// after the pane's size or attached clients changed. The first geometry
// seen only sets the baseline.
func (w *Watcher) selfRedraw(ctx context.Context, id string, e *entry, now time.Time) bool {
	if w.geometry != nil {
		if g, err := w.geometry(ctx, id); err == nil && g != e.geom {
			if e.geom != "" {
				e.quietUntil = now.Add(redrawGrace)
			}
			e.geom = g
		}
	}
	return now.Before(e.quietUntil)
}

// settle runs after silence: a prompt on screen is reported, otherwise the
// session is idle and its owner is told it is done.
func (w *Watcher) settle(ctx context.Context, id string, e *entry) {
	e.working, e.promptCheck, e.busyMisses = false, false, 0
	p, ok, err := w.inspect(ctx, id)
	switch {
	case err != nil && e.prompt != "":
		// The prompt may still be up; look again on the next tick.
		e.awaitingIdle = true
	case err != nil:
		w.setStatus(id, statusIdle)
	case ok:
		w.promptSeen(ctx, id, e, p)
	default:
		w.clearPrompt(ctx, id, e)
		w.setStatus(id, statusIdle)
		w.notify(id, wire.LevelDone, w.sessionName(ctx, id), "")
	}
}

// agentOutput handles output from an agent with a working indicator: it
// marks the session working only when the indicator is on screen.
func (w *Watcher) agentOutput(ctx context.Context, id string, e *entry, now time.Time) {
	e.lastOutput = now
	if e.working {
		e.awaitingIdle = true
		return
	}
	e.lastBusyCheck = now
	if !w.busy(ctx, id, e) {
		e.promptCheck = true
		return
	}
	e.busyMisses = 0
	if e.outputSeen(now) {
		w.setStatus(id, statusWorking)
	}
}

// plainOutput handles output from a program without a known indicator.
func (w *Watcher) plainOutput(id string, e *entry, now time.Time) {
	if e.burstStart.IsZero() || now.Sub(e.lastOutput) > burstGap {
		e.burstStart = now
	}
	e.lastOutput = now
	if e.working {
		e.awaitingIdle = true
		return
	}
	starting := !e.attachedAt.IsZero() && now.Sub(e.attachedAt) < startupGrace
	if starting || now.Sub(e.burstStart) < sustainedOutput {
		e.promptCheck = true
		return
	}
	if e.outputSeen(now) {
		w.setStatus(id, statusWorking)
	}
}

// turnOver reports whether a working agent with an indicator has stopped
// showing it on busyMissesToSettle reads in a row, so its turn ended even
// though redraws keep the pane from falling silent.
func (w *Watcher) turnOver(ctx context.Context, id string, e *entry, now time.Time) bool {
	if !e.working || !detect.KnowsBusy(e.agent) || now.Sub(e.lastBusyCheck) < busyCheckEvery {
		return false
	}
	e.lastBusyCheck = now
	if w.busy(ctx, id, e) {
		e.busyMisses = 0
		return false
	}
	e.busyMisses++
	return e.busyMisses >= busyMissesToSettle
}

// busy reads the screen for the agent's working indicator. A failed read
// counts as busy, so a tmux hiccup never ends a turn.
func (w *Watcher) busy(ctx context.Context, id string, e *entry) bool {
	screen, err := w.capture(ctx, id)
	if err != nil {
		return e.working
	}
	return detect.Busy(e.agent, screen)
}

// StatusReader reads back the status a StatusSink holds. A sink that
// implements it lets the watcher correct a stale "working".
type StatusReader interface {
	Status(id string) string
}

// checkPrompt looks for a prompt once output that did not mark the session
// working fell silent; without one it checks the status is not stale.
func (w *Watcher) checkPrompt(ctx context.Context, id string, e *entry) {
	p, ok, err := w.inspect(ctx, id)
	switch {
	case err != nil:
	case ok:
		w.promptSeen(ctx, id, e, p)
	default:
		w.staleWorking(ctx, id, e)
	}
}

// staleWorking ends a "working" status that the screen no longer bears
// out: an agent with a working indicator shows neither it nor a prompt.
// It happens when a turn is interrupted at a permission prompt: the
// prompt already settled the session without changing its status, the
// hooks that set "working" send no Stop after an interrupt, and the
// output the interrupt draws is no work. An interrupt is no finished
// turn, so no done notification goes out.
func (w *Watcher) staleWorking(ctx context.Context, id string, e *entry) {
	r, ok := w.cfg.Status.(StatusReader)
	if !ok || r.Status(id) != statusWorking || !detect.KnowsBusy(e.agent) || w.busy(ctx, id, e) {
		return
	}
	w.setStatus(id, statusIdle)
}
