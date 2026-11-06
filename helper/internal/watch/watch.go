// Package watch supervises the output tap of every managed session and turns
// the tap's signals into status changes, prompt reports and notifications.
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tap"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	defaultIdle    = 6 * time.Second
	reconcileEvery = 2 * time.Second
	tickEvery      = 500 * time.Millisecond
	statusWorking  = "working"
	statusIdle     = "idle"
	bellTitle      = "Bell"
	// attentionGap limits captures per session caused by bells and OSC
	// notifications; notifyGap limits repeats of the same notification.
	attentionGap = 500 * time.Millisecond
	notifyGap    = 30 * time.Second
)

// StatusSink receives status changes derived from pane activity.
type StatusSink interface {
	SetStatus(id, status string, activity *string)
}

// PromptSink is told when a prompt appears on a session's screen, when it
// is no longer there, and when the session itself is gone.
type PromptSink interface {
	PromptSeen(ctx context.Context, sessionID string, p detect.Prompt)
	PromptGone(ctx context.Context, sessionID string)
	SessionGone(ctx context.Context, sessionID string)
}

// Config wires a Watcher.
type Config struct {
	Tmux     *tmux.Client
	Sessions *session.Service
	// TogoPath is the absolute path of the helper binary the tap runs.
	TogoPath string
	// Home is baked into the tap command: tmux starts pipe-pane children
	// from the tmux server's environment, not the helper's. Empty means
	// state.Home().
	Home string
	// Idle is the silence after output before the screen is inspected.
	// Zero means TOGO_IDLE_SECONDS when set, else 6 seconds.
	Idle    time.Duration
	Clock   func() time.Time
	Status  StatusSink
	Prompts PromptSink
	Pub     wire.Publisher
}

// Watcher supervises taps and reacts to their signals.
type Watcher struct {
	cfg  Config
	idle time.Duration
	home string
	now  func() time.Time

	// Seams for tests; New binds them to tmux and the session service.
	list    func(context.Context) ([]session.Session, error)
	attach  func(context.Context, string) error
	capture func(context.Context, string) (string, error)
	// piped reports which sessions still have a live tap; nil skips the
	// check.
	piped func(context.Context) (map[string]bool, error)
	// armed reports the tap command recorded on each session; nil skips
	// the check, so every unattached session is attached.
	armed func(context.Context) (map[string]string, error)
	// agentGone checks the pane before an exit report counts.
	agentGone func(context.Context, string) (bool, error)
	// settledGone checks the pane for an agent gone without a report: gone,
	// in a pane that did not just start.
	settledGone func(context.Context, string) (bool, error)
	// pruneRecord prunes the restore record; nil skips it. pruned is
	// set after the first reconcile.
	pruneRecord func(context.Context) error
	pruned      bool
	// scans counts the reconciles that listed the sessions. The first
	// restores stored exits; the first two look for agents already gone.
	scans int

	mu       sync.Mutex
	sessions map[string]*entry
}

// entry serializes the handling of one session's signals.
type entry struct {
	mu sync.Mutex
	sessionState
}

// New returns a Watcher. It does not touch tmux until Run or Reconcile.
func New(cfg Config) (*Watcher, error) {
	w := &Watcher{cfg: cfg, idle: cfg.Idle, home: cfg.Home, now: cfg.Clock, sessions: map[string]*entry{}}
	if w.now == nil {
		w.now = time.Now
	}
	if w.idle <= 0 {
		w.idle = idleFromEnv()
	}
	if w.home == "" {
		h, err := state.Home()
		if err != nil {
			return nil, fmt.Errorf("watch: %w", err)
		}
		w.home = h
	}
	w.list = cfg.Sessions.List
	w.attach = w.attachTap
	w.capture = func(ctx context.Context, id string) (string, error) {
		return cfg.Tmux.CapturePane(ctx, "="+id+":")
	}
	w.piped = cfg.Tmux.PipedSessions
	w.armed = cfg.Tmux.TapCommands
	w.agentGone = w.paneGone
	w.settledGone = w.paneSettledGone
	w.pruneRecord = cfg.Sessions.Settle
	return w, nil
}

// SetIdle changes the idle window. TOGO_IDLE_SECONDS, when set, still wins,
// so tests that pin the window keep it.
func (w *Watcher) SetIdle(d time.Duration) {
	if d <= 0 || envIdleSet() {
		return
	}
	w.mu.Lock()
	w.idle = d
	w.mu.Unlock()
}

func (w *Watcher) idleWindow() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.idle
}

func envIdleSet() bool {
	n, err := strconv.ParseFloat(os.Getenv("TOGO_IDLE_SECONDS"), 64)
	return err == nil && n > 0
}

func idleFromEnv() time.Duration {
	if n, err := strconv.ParseFloat(os.Getenv("TOGO_IDLE_SECONDS"), 64); err == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	return defaultIdle
}

// Methods registers local.signal.
func (w *Watcher) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodLocalSignal: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var sig wire.Signal
			if err := json.Unmarshal(raw, &sig); err != nil {
				return nil, &session.InvalidParamsError{Msg: "invalid signal params"}
			}
			if err := w.Signal(ctx, sig); err != nil {
				return nil, err
			}
			return struct{}{}, nil
		},
	}
}

// Run reconciles taps every two seconds and checks for silence until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.Reconcile(ctx)
	reconcile := time.NewTicker(reconcileEvery)
	tick := time.NewTicker(tickEvery)
	defer reconcile.Stop()
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcile.C:
			w.Reconcile(ctx)
		case <-tick.C:
			w.Tick(ctx)
		}
	}
}

// Reconcile attaches the tap to managed sessions that lack one and reports
// sessions that have vanished. A failed listing changes nothing, so a tmux
// hiccup is never mistaken for every session ending.
func (w *Watcher) Reconcile(ctx context.Context) {
	all, err := w.list(ctx)
	if err != nil {
		return
	}
	// A tap that died (killed, or crashed on bad input) leaves the pane
	// unpiped; attaching again restores signals for the session.
	var piped map[string]bool
	if w.piped != nil {
		if p, err := w.piped(ctx); err == nil {
			piped = p
		}
	}
	// A tap armed by an earlier daemon with this binary's command keeps
	// running; one that names another binary path, such as a Homebrew
	// version directory removed by an upgrade, is armed again.
	var armed map[string]string
	if w.armed != nil && piped != nil {
		if a, err := w.armed(ctx); err == nil {
			armed = a
		}
	}
	w.mu.Lock()
	w.scans++
	scan := w.scans
	w.mu.Unlock()
	live := map[string]bool{}
	for _, s := range all {
		if !s.Managed {
			continue
		}
		live[s.ID] = true
		e := w.entry(s.ID)
		e.mu.Lock()
		if e.attached && piped != nil && !piped[s.ID] {
			e.attached = false
		}
		if !e.attached && piped[s.ID] && armed[s.ID] != "" && armed[s.ID] == w.tapCommand(s.ID) {
			e.attached = true
			e.screenCheck = true
		}
		if !e.attached && w.attach(ctx, s.ID) == nil {
			e.attached = true
			e.screenCheck = true
		}
		if scan <= startScans {
			w.restoreExit(ctx, s, e, scan)
		}
		e.mu.Unlock()
	}

	type vanished struct {
		id       string
		attached bool
	}
	var gone []vanished
	w.mu.Lock()
	for id, e := range w.sessions {
		if live[id] {
			continue
		}
		delete(w.sessions, id)
		e.mu.Lock()
		gone = append(gone, vanished{id, e.attached})
		e.mu.Unlock()
	}
	prune := w.pruneRecord != nil && (!w.pruned || len(gone) > 0)
	w.pruned = true
	w.mu.Unlock()
	if prune {
		// Sessions that vanished under a live server were ended at the
		// desk, not by a restart; the record must not offer them later.
		_ = w.pruneRecord(ctx)
	}
	for _, g := range gone {
		if w.cfg.Prompts != nil {
			w.cfg.Prompts.SessionGone(ctx, g.id)
		}
		if g.attached && w.cfg.Pub != nil {
			id := g.id
			w.cfg.Pub.Publish(wire.EventSessionRemoved, &id, wire.SessionRemovedEvent{})
		}
	}
}

func (w *Watcher) attachTap(ctx context.Context, id string) error {
	cmd, err := tap.PipeCommand(w.cfg.TogoPath, id)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(w.home) || strings.ContainsRune(w.home, 0) {
		return fmt.Errorf("watch: home %q is not a usable absolute path", w.home)
	}
	full := pipeCommand(w.home, cmd)
	if err := w.cfg.Tmux.PipePane(ctx, "="+id+":", full); err != nil {
		return err
	}
	// Best effort: without the record the next daemon arms the tap again.
	_ = w.cfg.Tmux.SetOption(ctx, id, tmux.TapOption, full)
	return nil
}

// tapCommand is the pipe-pane command attachTap gives tmux for id, or ""
// when it cannot be built.
func (w *Watcher) tapCommand(id string) string {
	cmd, err := tap.PipeCommand(w.cfg.TogoPath, id)
	if err != nil || !filepath.IsAbs(w.home) {
		return ""
	}
	return pipeCommand(w.home, cmd)
}

func pipeCommand(home, tapCmd string) string {
	return "TOGO_HOME=" + shellQuote(home) + " " + tapCmd
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (w *Watcher) entry(id string) *entry {
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.sessions[id]
	if e == nil {
		e = &entry{}
		w.sessions[id] = e
	}
	return e
}

func (w *Watcher) snapshot() map[string]*entry {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]*entry, len(w.sessions))
	for id, e := range w.sessions {
		out[id] = e
	}
	return out
}

// Tick inspects every session whose output has been silent for the idle
// window, and every session whose tap was just attached.
func (w *Watcher) Tick(ctx context.Context) {
	now, idle := w.now(), w.idleWindow()
	for id, e := range w.snapshot() {
		e.mu.Lock()
		switch {
		case e.silent(now, idle) && w.Ended(id):
			// Silence after the agent ended or failed is not a done:
			// neither for output its hooks saw before the exit, nor for
			// output from the shell the pane fell back to.
			e.awaitingIdle, e.working = false, false
		case e.silent(now, idle):
			e.awaitingIdle = false
			e.screenCheck = false
			w.settle(ctx, id, e)
		// Output since the attach leaves the screen to the idle check.
		case e.screenCheck && !e.awaitingIdle:
			e.screenCheck = false
			if p, ok, err := w.inspect(ctx, id); err == nil && ok {
				w.promptSeen(ctx, id, e, p)
			}
		}
		e.mu.Unlock()
	}
}

// settle runs after silence: a prompt on screen is reported, otherwise the
// session is idle and its owner is told it is done.
func (w *Watcher) settle(ctx context.Context, id string, e *entry) {
	e.working = false
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

// Signal handles one signal from a tap or a launch wrapper.
func (w *Watcher) Signal(ctx context.Context, sig wire.Signal) error {
	if managed, err := session.ValidateID(sig.SessionID); err != nil {
		return err
	} else if !managed {
		return &session.InvalidParamsError{Msg: "signals are accepted for managed sessions only"}
	}
	e := w.entry(sig.SessionID)
	e.mu.Lock()
	defer e.mu.Unlock()

	switch sig.Kind {
	case wire.SignalOutput:
		w.output(ctx, sig.SessionID, e)
	case wire.SignalBell, wire.SignalOSC9, wire.SignalOSC777:
		w.attention(ctx, sig, e)
	case wire.SignalExit:
		return w.exit(ctx, sig, e)
	default:
		return &session.InvalidParamsError{Msg: "unknown signal kind"}
	}
	return nil
}

func (w *Watcher) output(ctx context.Context, id string, e *entry) {
	if w.Ended(id) {
		return
	}
	if e.prompt != "" {
		p, ok, err := w.inspect(ctx, id)
		if err != nil {
			e.lastOutput, e.awaitingIdle = w.now(), true
			return
		}
		if ok && p.Fingerprint == e.prompt {
			return
		}
		w.clearPrompt(ctx, id, e)
	}
	if e.outputSeen(w.now()) {
		w.setStatus(id, statusWorking)
	}
}

func (w *Watcher) attention(ctx context.Context, sig wire.Signal, e *entry) {
	id := sig.SessionID
	now := w.now()
	if now.Sub(e.lastInspect) < attentionGap {
		return
	}
	e.lastInspect = now
	p, ok, err := w.inspect(ctx, id)
	if err == nil && ok {
		w.promptSeen(ctx, id, e, p)
		return
	}
	title := sig.Title
	if title == "" {
		title = bellTitle
	}
	if title == e.notifyTitle && sig.Body == e.notifyBody && now.Sub(e.lastNotify) < notifyGap {
		return
	}
	e.lastNotify, e.notifyTitle, e.notifyBody = now, title, sig.Body
	w.notify(id, wire.LevelNeedsYou, title, sig.Body)
}

func (w *Watcher) inspect(ctx context.Context, id string) (detect.Prompt, bool, error) {
	screen, err := w.capture(ctx, id)
	if err != nil {
		return detect.Prompt{}, false, err
	}
	p, ok := detect.Detect(screen)
	return p, ok, nil
}

func (w *Watcher) promptSeen(ctx context.Context, id string, e *entry, p detect.Prompt) {
	if e.prompt == p.Fingerprint {
		return
	}
	e.prompt = p.Fingerprint
	if w.cfg.Prompts != nil {
		w.cfg.Prompts.PromptSeen(ctx, id, p)
	}
}

func (w *Watcher) clearPrompt(ctx context.Context, id string, e *entry) {
	if e.prompt == "" {
		return
	}
	e.prompt = ""
	if w.cfg.Prompts != nil {
		w.cfg.Prompts.PromptGone(ctx, id)
	}
}

func (w *Watcher) setStatus(id, status string) {
	if w.cfg.Status != nil {
		w.cfg.Status.SetStatus(id, status, nil)
	}
}

func (w *Watcher) notify(id, level, title, body string) {
	if w.cfg.Pub != nil {
		w.cfg.Pub.Publish(wire.EventNotify, &id, wire.NotifyEvent{Title: title, Body: body, Level: level})
	}
}

// sessionName is the notification title for a finished session.
func (w *Watcher) sessionName(ctx context.Context, id string) string {
	all, err := w.list(ctx)
	if err == nil {
		for _, s := range all {
			if s.ID == id && s.Name != "" {
				return s.Name
			}
		}
	}
	return "Session"
}
