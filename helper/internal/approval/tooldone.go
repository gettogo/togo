package approval

import (
	"context"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// CloseAnswered closes the session's pending prompt approval as answered
// at the desk unless its prompt is still on the screen. A finished tool
// says the agent moved on from that tool, not from every prompt: with
// tools running side by side, one can finish while another still waits
// for permission, and that request must stay answerable from the phone.
func (s *Store) CloseAnswered(ctx context.Context, sessionID string) {
	mu := s.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.mu.Lock()
	cur := s.pendingForSession(sessionID)
	if cur == nil {
		s.mu.Unlock()
		return
	}
	id, fingerprint, hook, agent := cur.a.RequestID, cur.fingerprint, cur.hookSubject, cur.a.Agent
	s.mu.Unlock()
	if screen, err := s.cfg.Tmux.CapturePane(ctx, target(sessionID)); err == nil {
		// An approval a hook opened has no fingerprint until its prompt
		// is read: a prompt showing may be its own if it asks about the
		// same thing.
		if p, ok := detect.Detect(screen); ok && (fingerprint == p.Fingerprint || (fingerprint == "" && hookOwns(hook, agent, p.Subject))) {
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok && e.state == statePending {
		s.closeLocked(e, wire.ResolutionDesk)
	}
}
