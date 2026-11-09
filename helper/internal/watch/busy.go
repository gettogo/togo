package watch

import (
	"context"
	"crypto/sha256"
	"strings"
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
//     since the tap attached and output has run for sustainedOutput;
//   - for a coding agent without an indicator, output that leaves the
//     screen's text as it was is no output at all: Vibe draws its own
//     blinking cursor at the idle input box, so its pane never falls
//     silent.
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
	// textIdlePoll spaces the screen reads of an agent whose output left
	// its text unchanged last time (a blinking cursor at an idle prompt).
	textIdlePoll = 8 * time.Second
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
	if e.prompt == "" && w.textUnchanged(ctx, id, e) {
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
			if w.askedAgain(id, e, p) {
				w.promptSeen(ctx, id, e, p)
			}
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

// textUnchanged reports whether output from a coding agent without a
// working indicator left the screen's text as it last was: a cursor it
// draws itself blinking (Vibe), not work. The screen is read at most every
// promptPoll, and every textIdlePoll once it was found unchanged; output in
// between gets the last read's verdict. Plain shells, custom commands and
// agents with an indicator are not read here, and a failed read counts as
// a change.
func (w *Watcher) textUnchanged(ctx context.Context, id string, e *entry) bool {
	if !codingAgent(e.agent) || detect.KnowsBusy(e.agent) {
		return false
	}
	every := promptPoll
	if e.textSame {
		every = textIdlePoll
	}
	if !e.textAt.IsZero() && w.now().Sub(e.textAt) < every {
		return e.textSame
	}
	_, err := w.readText(ctx, id, e)
	return err == nil && e.textSame
}

// readText captures the screen and notes whether its text changed since
// the last read.
func (w *Watcher) readText(ctx context.Context, id string, e *entry) (string, error) {
	e.textAt = w.now()
	screen, err := w.capture(ctx, id)
	if err != nil {
		e.textSame = false
		return "", err
	}
	sum := screenSum(e.agent, screen)
	e.textSame = e.textSum == sum
	e.textSum = sum
	return screen, nil
}

// inspectText is inspect for a coding agent's redraw poll: the read also
// serves textUnchanged, so the two do not both read the screen.
func (w *Watcher) inspectText(ctx context.Context, id string, e *entry) (detect.Prompt, bool, error) {
	if detect.KnowsBusy(e.agent) {
		return w.inspect(ctx, id)
	}
	screen, err := w.readText(ctx, id, e)
	if err != nil {
		return detect.Prompt{}, false, err
	}
	p, ok := detect.Detect(screen)
	return p, ok, nil
}

// decor is the animation an agent draws at the start of its lines while it
// waits: Amp's welcome screen spins an orb of dots beside "Welcome to Amp".
var decor = map[string]string{"amp": " •●·.:"}

// screenSum is the screen's text for telling work from animation: without
// the run of decor characters that starts each line, for an agent that
// draws one.
func screenSum(agent, screen string) [sha256.Size]byte {
	cut := decor[agent]
	if cut == "" {
		return sha256.Sum256([]byte(screen))
	}
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimLeft(l, cut)
	}
	return sha256.Sum256([]byte(strings.Join(lines, "\n")))
}
