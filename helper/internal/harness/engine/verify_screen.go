package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// readScreenValue is readValue from the pane, with what the screen
// readers found there.
func (e *Engine) readScreenValue(ctx context.Context, s *sess, v *spec.Verify) (string, bool, map[string]string) {
	if v == nil || v.Select == "" || e.cfg.Screen == nil {
		return "", false, nil
	}
	s.mu.Lock()
	state := s.stateLocked()
	var readers []ScreenReader
	for _, m := range s.mappers {
		if r, ok := m.(ScreenReader); ok {
			readers = append(readers, r)
		}
	}
	s.mu.Unlock()
	if len(readers) == 0 {
		return "", false, nil
	}
	text, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return "", false, nil
	}
	read := map[string]string{}
	for _, r := range readers {
		for k, val := range r.ReadScreen(text) {
			read[k] = val
			state[k] = val
		}
	}
	if len(read) == 0 {
		return "", false, nil
	}
	out, err := JQFirst(v.Select, stateAny(state), state)
	if err != nil || out == nil {
		return "", false, nil
	}
	return toString(out), true, read
}

// notConfirmed says a required change did not show, and what the harness
// shows instead when it can be read.
func (e *Engine) notConfirmed(ctx context.Context, s *sess, v *spec.Verify) string {
	msg := s.ad.Name + " did not take the change."
	got, ok := e.readValue(ctx, s, v, false)
	if !ok || got == "" {
		got, ok, _ = e.readScreenValue(ctx, s, v)
	}
	if ok && got != "" {
		msg += " It still uses " + got + "."
	}
	return msg
}
