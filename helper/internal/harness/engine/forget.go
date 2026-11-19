package engine

import (
	"os"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Forget stops serving a session and drops its state, on disk too: the
// session was removed.
func (e *Engine) Forget(id string) {
	if s := e.unload(id); s != nil {
		s.mu.Lock()
		if h := s.hookStoreLocked(); h != nil {
			h.remove(true)
		}
		s.mu.Unlock()
	} else if p := hookStorePath(e.cfg.TogoHome, id); p != "" {
		_ = os.Remove(p)
	}
	e.forgetConversation(id)
	e.convCarry.take(id)
	unlock := e.endpointOps.lock(id)
	e.retireEndpoint(id)
	e.dropRelaunch(id)
	unlock()
	typing.Delete(id)
	draining.Delete(id)
}

// Unload stops serving a session and drops what the engine holds of it in
// memory, as the helper's exit does; what it keeps under run/ for a
// restart stays. Serving the session again restores from there.
func (e *Engine) Unload(id string) {
	e.unload(id)
	e.convCarry.take(id)
	e.dropRelaunch(id)
	typing.Delete(id)
	draining.Delete(id)
}

func (e *Engine) unload(id string) *sess {
	e.forgetEntered(id)
	e.mu.Lock()
	s, ok := e.sessions[id]
	delete(e.sessions, id)
	e.mu.Unlock()
	if ok {
		s.stop()
		e.menuMu.Lock()
		delete(e.catalogs, s)
		e.menuMu.Unlock()
	}
	e.log.Forget(id)
	e.cache.forget(id)
	source.Hooks.Forget(id)
	if !ok {
		return nil
	}
	return s
}
