package approval

import (
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// A permission hook often fires before its prompt is drawn, so its
// approval has no options until the watcher reads the prompt and the
// approval adopts it. The prompt read next is not always the hook's own:
// the user can answer it at the desk between two looks and the agent move
// on to another. Adopting that one would show the phone one command with
// buttons that answer another. So a hook approval takes a prompt only when
// the prompt asks about what the hook asked about (detect.SameSubject on
// the subject the detector reads, the same one the prompt's fingerprint
// covers). When unsure it does not: the prompt then opens a request of its
// own, as for an agent with no permission hook, and the hook's request
// closes without ever becoming answerable.
//
// A hook with no subject adopts only for Claude Code, whose permission
// notification is sent for the dialog on screen and comes with it.

// hookOwns reports whether a prompt asking about screen is the one a
// permission hook from agent asked about.
func hookOwns(hook *wire.Subject, agent string, screen *wire.Subject) bool {
	if hook != nil {
		return detect.SameSubject(hook, screen)
	}
	return agent == "claude"
}

// withEntry runs f on the request's entry under s.mu.
func (s *Store) withEntry(requestID string, f func(*entry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[requestID]; ok {
		f(e)
	}
}
