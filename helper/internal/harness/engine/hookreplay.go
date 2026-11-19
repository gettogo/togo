package engine

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness"
)

// hooksOnly reports whether the session's chat comes from hooks alone:
// every feed is a hook or a status line. Its hook records are stored
// (hookstore.go); an agent with a transcript or a server is read back from
// those after a restart instead.
func (s *sess) hooksOnly() bool {
	hook := false
	for _, f := range s.ad.Feeds {
		switch {
		case f.Hook != nil:
			hook = true
		case f.Statusline != nil:
		default:
			return false
		}
	}
	return hook
}

// hookStoreLocked returns the session's hook store, nil when its records
// are not kept.
func (s *sess) hookStoreLocked() *hookStore {
	if s.hooks == nil {
		path := ""
		if s.ad != nil && s.hooksOnly() {
			path = hookStorePath(s.e.cfg.TogoHome, s.id)
		}
		s.hooks = &hookStore{path: path}
	}
	if s.hooks.path == "" {
		return nil
	}
	return s.hooks
}

// replayHooksLocked restores a hook feed's stored records once, when the
// feed first starts in this process, before any live record is mapped: a
// replay, so nothing of the past is announced again.
func (s *sess) replayHooksLocked(feed, mapper string) {
	h := s.hookStoreLocked()
	if h == nil || s.hookReplayed[feed] {
		return
	}
	if s.hookReplayed == nil {
		s.hookReplayed = map[string]bool{}
	}
	s.hookReplayed[feed] = true
	lines := h.load(s.e.cfg.Now())
	rec, ok := replayRecord(feed, lines)
	if !ok {
		return
	}
	if from := s.restoreLocked(s.ctx, mapper, rec); from > 0 {
		// The store spans a move to another conversation: only what the
		// chat now shows is kept, so the next restart does not read the
		// conversation left again.
		h.remove(false)
		n := 0
		for _, l := range lines {
			if l.Feed == feed {
				n++
				if n <= from {
					continue
				}
			}
			if err := h.add(l); err != nil {
				s.e.cfg.Logf("harness: %s: a hook record could not be kept: %v", s.ad.Adapter.Adapter, err)
			}
		}
	}
}

// keepHookLocked stores a live hook record as it was mapped.
func (s *sess) keepHookLocked(rec harness.Record, pre, post json.RawMessage) {
	if f, ok := s.ad.Feeds[rec.Source]; !ok || f.Hook == nil {
		return
	}
	h := s.hookStoreLocked()
	if h == nil {
		return
	}
	at := rec.At
	if at.IsZero() {
		at = s.e.cfg.Now()
	}
	if err := h.add(hookLine{Feed: rec.Source, At: at, Data: rec.Data, Pre: pre, Post: post}); err != nil {
		s.e.cfg.Logf("harness: %s: a hook record could not be kept for a restart: %v", s.id, err)
	}
}

// replayMark is where a mapper stands, nil for one that is no ReplayMarker.
func replayMark(m harness.Mapper) json.RawMessage {
	if mk, ok := m.(harness.ReplayMarker); ok {
		return mk.ReplayMark()
	}
	return nil
}
