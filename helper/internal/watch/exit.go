package watch

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ExitSink records that a session's agent exited. The hub implements it;
// a Config.Status that does too receives exits, and the watcher asks it
// whether a session has ended.
type ExitSink interface {
	SetExit(id string, status string, exit *wire.SessionExit)
	Ended(id string) bool
}

// Exit statuses the wrapper reports for a stop by Ctrl-C (SIGINT) and by
// SIGTERM.
const (
	codeInterrupted = 130
	codeTerminated  = 143
)

// ExitFor maps an agent's exit status to the session status and exit the
// app sees: 0 ended normally, 130 and 143 were interrupted, anything else
// failed.
func ExitFor(code int) (string, wire.SessionExit) {
	c := code
	switch code {
	case 0:
		return wire.StatusEnded, wire.SessionExit{Code: &c, Reason: wire.ExitReasonExit}
	case codeInterrupted, codeTerminated:
		return wire.StatusEnded, wire.SessionExit{Code: &c, Reason: wire.ExitReasonInterrupted}
	}
	return wire.StatusFailed, wire.SessionExit{Code: &c, Reason: wire.ExitReasonExit}
}

// ExitRestorer records an exit the daemon did not see happen, which sends
// no push. The hub implements it.
type ExitRestorer interface {
	RestoreExit(id string, status string, exit *wire.SessionExit)
}

// startScans is how many reconciles after the daemon starts look for
// agents already gone; one must see it on every one of them.
const startScans = 2

// paneGrace is how long a pane must have run before an agent missing from
// it counts as gone without a report: a pane just created or respawned
// shows only its login shell until the agent starts.
const paneGrace = 10 * time.Second

// restoreExit runs for each session on the first startScans reconciles
// after the daemon starts. On the first, an exit stored before the restart
// is restored. An agent session whose pane shows the agent gone on each of
// those scans (its exit report was lost, or it predates the wrapper) is
// restored as ended: agents without hooks have nothing to clear a false
// mark. Either way the output of the shell the pane fell back to is never
// taken for work.
func (w *Watcher) restoreExit(ctx context.Context, s session.Session, e *entry, scan int) {
	r, ok := w.cfg.Status.(ExitRestorer)
	if !ok || w.Ended(s.ID) {
		return
	}
	if saved := s.SavedExit; saved != nil && scan == 1 {
		exit := saved.Exit
		r.RestoreExit(s.ID, saved.Status, &exit)
		return
	}
	if s.Command == nil || w.settledGone == nil {
		return
	}
	if gone, err := w.settledGone(ctx, s.ID); err != nil || !gone {
		e.goneScans = 0
		return
	}
	if e.goneScans++; e.goneScans < startScans {
		return
	}
	e.working, e.awaitingIdle = false, false
	r.RestoreExit(s.ID, wire.StatusEnded, &wire.SessionExit{Reason: wire.ExitReasonExit})
}

// paneSettledGone is the production settledGone check.
func (w *Watcher) paneSettledGone(ctx context.Context, id string) (bool, error) {
	fg, err := w.cfg.Tmux.Foreground(ctx, id)
	if err != nil {
		return false, err
	}
	return SettledGone(fg, w.cfg.TogoPath), nil
}

// SettledGone reports whether fg shows the agent gone in a pane that has
// run for at least paneGrace, or is dead.
func SettledGone(fg tmux.PaneForeground, togoPath string) bool {
	return session.AgentGone(fg, togoPath) && (fg.Dead || fg.PaneAge >= paneGrace)
}

func (w *Watcher) exits() ExitSink {
	x, _ := w.cfg.Status.(ExitSink)
	return x
}

// Ended reports whether the session's agent has exited and nothing has
// started it again.
func (w *Watcher) Ended(id string) bool {
	x := w.exits()
	return x != nil && x.Ended(id)
}

// exit handles the launch wrapper's report. Any process of the user's can
// send one for any session, so it counts only when the pane shows the
// agent gone: the pane is dead, or its foreground job is the fallback
// shell or the helper itself sending the report. Anything else, such as
// the agent's own tool running `togo session-exit`, is ignored.
func (w *Watcher) exit(ctx context.Context, sig wire.Signal, e *entry) error {
	if sig.Code == nil || *sig.Code < 0 || *sig.Code > 255 {
		return &session.InvalidParamsError{Msg: "an exit signal needs a code of 0-255"}
	}
	x := w.exits()
	if x == nil {
		return nil
	}
	if w.agentGone == nil {
		return nil
	}
	if gone, err := w.agentGone(ctx, sig.SessionID); err != nil || !gone {
		return nil
	}
	status, ex := ExitFor(*sig.Code)
	e.working, e.awaitingIdle = false, false
	w.clearPrompt(ctx, sig.SessionID, e)
	x.SetExit(sig.SessionID, status, &ex)
	return nil
}

// paneGone is the production agentGone check.
func (w *Watcher) paneGone(ctx context.Context, id string) (bool, error) {
	fg, err := w.cfg.Tmux.Foreground(ctx, id)
	if err != nil {
		return false, err
	}
	return session.AgentGone(fg, w.cfg.TogoPath), nil
}
