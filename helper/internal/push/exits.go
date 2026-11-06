package push

import (
	"strconv"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// exitChanged reports whether sess carries an exit that has not been
// announced yet, and records it. A session that is neither ended nor failed
// forgets its exit, so the next one is announced again. A restored exit is
// recorded but never announced: it was announced before a restart, or
// happened while nothing was watching.
func (s *Sender) exitChanged(sess session.Session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (sess.Status != wire.StatusEnded && sess.Status != wire.StatusFailed) || sess.Exit == nil {
		delete(s.exits, sess.ID)
		return false
	}
	key := sess.Status + "/" + sess.Exit.Reason
	if sess.Exit.Code != nil {
		key += "/" + strconv.Itoa(*sess.Exit.Code)
	}
	if s.exits[sess.ID] == key {
		return false
	}
	s.exits[sess.ID] = key
	return !sess.ExitRestored
}
