package approval

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// expire closes a still-pending approval at its TTL and later forgets it,
// once a late answer no longer needs to learn the outcome. A prompt that
// is still on the screen keeps its approval instead: the agent is still
// blocked, and someone away for longer than the TTL must still be able to
// answer it and be reminded of it.
func (s *Store) expire(requestID string) {
	if s.extend(requestID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[requestID]
	if !ok {
		return
	}
	if e.state == statePending {
		s.closeLocked(e, wire.ResolutionExpired)
		time.AfterFunc(s.cfg.TTL, func() { s.forget(requestID) })
		return
	}
	delete(s.entries, requestID)
}

// extend gives a pending screen or hook approval another TTL when its
// prompt is still shown, and announces the new expiry. Asks are not on the
// pane: their caller's timeout decides.
func (s *Store) extend(requestID string) bool {
	s.mu.Lock()
	e, ok := s.entries[requestID]
	if !ok || e.state != statePending || e.a.Source == wire.SourceAsk || e.a.SessionID == nil {
		s.mu.Unlock()
		return false
	}
	sessionID, fingerprint := *e.a.SessionID, e.fingerprint
	s.mu.Unlock()

	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	screen, err := s.cfg.Tmux.CapturePane(ctx, target(sessionID))
	if err != nil {
		return false
	}
	// An approval a hook opened has no fingerprint until its prompt is
	// read: any prompt showing may be its own.
	if p, shown := detect.Detect(screen); !shown || (fingerprint != "" && fingerprint != p.Fingerprint) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok = s.entries[requestID]; !ok || e.state != statePending {
		return false
	}
	exp := wire.Stamp(s.cfg.Clock()).Add(s.cfg.TTL)
	e.a.ExpiresAt = &exp
	time.AfterFunc(s.cfg.TTL, func() { s.expire(requestID) })
	s.cfg.Pub.Publish(wire.EventApprovalOpen, e.a.SessionID, openEvent(e))
	return true
}
