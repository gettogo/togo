package engine

import "github.com/gettogo/togo/helper/internal/harness"

// woken reads a session's mapper state again after a Waker learned
// something off a record: feeds follow a path or parameter it changed and
// working is read again, as after a record.
func (e *Engine) woken(id string) {
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	if s == nil {
		return
	}
	select {
	case <-s.ready:
	default:
		return // not started: its start reads the state
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	for _, m := range s.mappers {
		if w, ok := m.(harness.Waker); ok {
			if items, ops := w.Woken(); len(items) > 0 || len(ops) > 0 {
				s.publishLocked(items, ops)
			}
		}
	}
	s.afterRecordLocked()
}
