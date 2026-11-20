package engine

import (
	"context"
	"maps"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// paneMovedSlack is how far before a restart the helper noticed (an agent
// that had exited, run again by session.relaunch) a file named after the
// pane's new device may have been written and still be the new run's.
const paneMovedSlack = 2 * time.Second

// paneMoved reads the terminal device of the session's pane again after the
// agent was restarted in it, and gives the mappers that read files named
// after the device (harness.PaneMover) the new environment. tmux respawns
// the pane on a new device; omp then writes its record of the session file
// under that device, and the old device's record names the conversation
// before the restart. at is when the restart began. The device is asked
// outside the session lock. With onlyChanged, a device that did not change
// changes nothing: no restart is known to have happened.
//
// s.env stays as built: feeds and actions read it without the lock. The
// device and the restart time are kept in s.pane and given to mappers,
// the ones made later included (mapperEnvLocked).
func (e *Engine) paneMoved(s *sess, at time.Time, onlyChanged bool) {
	tty := ""
	if e.cfg.PaneTTY != nil && s.ctx != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		if t, err := e.cfg.PaneTTY(ctx, s.id); err == nil {
			tty = t
		}
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if onlyChanged && (tty == "" || tty == s.mapperEnvLocked().Vars[source.VarTTY]) {
		return
	}
	// Unknown: no device's record is read, rather than the old one.
	s.pane = &paneState{tty: tty, at: at}
	env := s.mapperEnvLocked()
	for _, m := range s.mappers {
		if p, ok := m.(harness.PaneMover); ok {
			p.PaneMoved(env)
		}
	}
}

// paneState is the pane's device and when the agent was last restarted in
// it, once the helper restarted it. It lives as long as the session's
// served state: a helper restart reads the device from tmux again.
type paneState struct {
	tty string
	at  time.Time
}

// mapperEnvLocked is the environment mappers are made with: the session's,
// with the pane's device as of the last restart. The caller holds s.mu.
func (s *sess) mapperEnvLocked() harness.Env {
	if s.pane == nil {
		return s.env
	}
	env := s.env
	env.Vars = maps.Clone(s.env.Vars)
	if env.Vars == nil {
		env.Vars = map[string]string{}
	}
	if s.pane.tty != "" {
		env.Vars[source.VarTTY] = s.pane.tty
	} else {
		delete(env.Vars, source.VarTTY)
	}
	env.Vars[source.VarRelaunched] = s.pane.at.UTC().Format(time.RFC3339Nano)
	return env
}
