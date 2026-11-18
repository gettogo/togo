package watch

import (
	"context"
	"path/filepath"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// ExitClearer takes back an exit: the agent runs again in its pane. The
// hub implements it.
type ExitClearer interface {
	ClearExit(id string)
}

// revive clears the exit of a session whose agent someone started again
// by hand in the shell its pane fell back to. Agents with hooks clear it
// themselves; one without would stay ended for good, refusing every send
// while it runs. Only the agent's own program in the foreground counts,
// so a command typed into that shell does not bring the session back.
func (w *Watcher) revive(ctx context.Context, s session.Session) {
	c, ok := w.cfg.Status.(ExitClearer)
	if !ok || len(s.Command) == 0 || w.agentBack == nil || !w.Ended(s.ID) {
		return
	}
	if back, err := w.agentBack(ctx, s.ID, s.Command[0]); err != nil || !back {
		return
	}
	c.ClearExit(s.ID)
}

// paneAgentBack is the production agentBack check.
func (w *Watcher) paneAgentBack(ctx context.Context, id, program string) (bool, error) {
	fg, err := w.cfg.Tmux.Foreground(ctx, id)
	if err != nil {
		return false, err
	}
	return AgentBack(fg, program), nil
}

// AgentBack reports whether fg shows program running in the foreground of
// a live pane, as a process other than the pane's own.
func AgentBack(fg tmux.PaneForeground, program string) bool {
	name := filepath.Base(program)
	if fg.Dead || fg.PanePID < 1 || name == "." || name == "/" {
		return false
	}
	for _, p := range fg.Foreground {
		if p.PID == fg.PanePID {
			continue
		}
		// Linux reports at most 15 bytes of a process name.
		if p.Name == name || (len(p.Name) == 15 && len(name) > 15 && name[:15] == p.Name) {
			return true
		}
	}
	return false
}
