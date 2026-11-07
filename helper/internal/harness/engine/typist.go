package engine

import (
	"context"
	"errors"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// SessionTypist types through the session service's sendText path (one
// bracketed paste, then Enter) and sends allow-listed keys with tmux.
type SessionTypist struct {
	Sessions *session.Service
	Tmux     *tmux.Client
	// Status is the hub's status, so sendText refuses the same sessions it
	// refuses for the reply bar.
	Status func(id string) string
}

var _ harness.Typist = SessionTypist{}

// Type pastes text into the session's pane. whenIdle is handled by the
// engine, which knows the agent's state.
func (t SessionTypist) Type(ctx context.Context, id, text string, submit, _ bool) error {
	status := ""
	if t.Status != nil {
		status = t.Status(id)
	}
	if status == "working" {
		// sendText's own refusal is about approvals and exits; a working
		// agent accepts typed input.
		status = ""
	}
	return t.Sessions.SendText(ctx, id, text, submit, status, nil)
}

// Keys sends named keys from the allow list.
func (t SessionTypist) Keys(ctx context.Context, id string, keys ...string) error {
	if _, err := session.ValidateID(id); err != nil {
		return err
	}
	for _, k := range keys {
		if !spec.AllowedKeys[k] {
			return errors.New("harness: key not allowed")
		}
	}
	return t.Tmux.SendKeys(ctx, "="+id+":", false, keys...)
}
