package engine

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
	if s.ctx.Err() != nil || !s.gateOK {
		return
	}
	s.afterRecordLocked()
}
