package engine

import (
	"context"
	"maps"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// PaneInfo is the pane a session's agent runs in.
type PaneInfo struct {
	TTY     string // terminal device, such as /dev/ttys011
	PID     int    // the process tmux started in the pane
	Command string // the command it was started or respawned with
	// Started is when PID started; zero when unknown.
	Started time.Time
}

// paneVarKeys are the variables paneVars sets; "" removes one.
var paneVarKeys = []string{source.VarTTY, source.VarPanePID, source.VarPaneStarted, source.VarPaneCommand}

// paneVars reads the session's pane: every key of paneVarKeys, "" for
// what is not known. It runs tmux and must not be called under a lock.
func (e *Engine) paneVars(ctx context.Context, id string) map[string]string {
	out := make(map[string]string, len(paneVarKeys))
	for _, k := range paneVarKeys {
		out[k] = ""
	}
	if e.cfg.Pane == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := e.cfg.Pane(ctx, id)
	if err != nil || p.TTY == "" {
		return out
	}
	out[source.VarTTY], out[source.VarPaneCommand] = p.TTY, p.Command
	if p.PID > 0 {
		out[source.VarPanePID] = strconv.Itoa(p.PID)
	}
	if !p.Started.IsZero() {
		out[source.VarPaneStarted] = p.Started.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// paneMoved reads the session's pane again after the agent may have been
// restarted in it, and gives the mappers that read files named after the
// pane's terminal device (harness.PaneMover) the new environment. tmux
// respawns a pane on a new device, and omp then writes its record of the
// session file under that device; the old device's record still names
// the conversation before the restart. With onlyChanged, a pane with the
// same device and process changes nothing. tmux is asked outside the
// session lock.
//
// s.env stays as built: feeds and actions read it without the lock. The
// pane is kept in s.pane and given to mappers, the ones made later
// included (mapperEnvLocked).
func (e *Engine) paneMoved(s *sess, onlyChanged bool) {
	if s.ctx == nil {
		return
	}
	vars := e.paneVars(s.ctx, s.id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if onlyChanged {
		cur := s.mapperEnvLocked().Vars
		if vars[source.VarTTY] == "" || vars[source.VarTTY] == cur[source.VarTTY] && vars[source.VarPanePID] == cur[source.VarPanePID] {
			return
		}
	}
	// A pane that could not be read reads no device's record, rather
	// than the old one.
	s.pane = vars
	env := s.mapperEnvLocked()
	for _, m := range s.mappers {
		if p, ok := m.(harness.PaneMover); ok {
			p.PaneMoved(env)
		}
	}
}

// mapperEnvLocked is the environment mappers are made with: the session's,
// with its pane as last read (paneMoved). The caller holds s.mu.
func (s *sess) mapperEnvLocked() harness.Env {
	if s.pane == nil {
		return s.env
	}
	env := s.env
	env.Vars = maps.Clone(s.env.Vars)
	if env.Vars == nil {
		env.Vars = map[string]string{}
	}
	for k, v := range s.pane {
		if v == "" {
			delete(env.Vars, k)
		} else {
			env.Vars[k] = v
		}
	}
	return env
}

// paneCheckEvery is how often a session whose mappers read per-device
// files checks that its pane still runs the same process: a respawn by
// hand (tmux respawn-pane -k) moves the pane to another device without an
// exit the hub would see.
var paneCheckEvery = 5 * time.Second

// watchPane runs paneMoved every paneCheckEvery until the session stops,
// for sessions with a harness.PaneMover mapper, except while an action
// relaunches the agent (that one reads the pane itself).
func (s *sess) watchPane() {
	t := time.NewTicker(s.e.scaled(paneCheckEvery))
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		if !s.relaunching.Load() {
			s.e.paneMoved(s, true)
		}
	}
}

// hasPaneMoverLocked reports whether a mapper reads per-device files.
func (s *sess) hasPaneMoverLocked() bool {
	for _, m := range s.mappers {
		if _, ok := m.(harness.PaneMover); ok {
			return true
		}
	}
	return false
}
