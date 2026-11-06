package push

import (
	"context"

	"github.com/gettogo/togo/helper/internal/wire"
)

// afterDeliver arms the reminder for a needs_you that reached a device, and
// cancels it when the request is withdrawn.
func (s *Sender) afterDeliver(m Message, res []Result, sid string) {
	if s.Remind == nil {
		return
	}
	switch m.Plaintext.Kind {
	case wire.PushClear:
		s.Remind.Cancel(m.Plaintext.RequestID)
	case wire.PushNeedsYou:
		for _, r := range res {
			if r.Err == nil {
				s.Remind.Schedule(m)
				return
			}
		}
	}
}

// cancelReminders handles events that produce no push but end a session's
// requests.
func (s *Sender) cancelReminders(ev wire.Event, sid string) {
	switch pl := deref(ev.Payload).(type) {
	case wire.SessionRemovedEvent:
		s.Remind.CancelSession(sid)
	case wire.SessionEvent:
		if sess, ok := sessionOf(pl.Session); ok && (sess.Status == wire.StatusEnded || sess.Status == wire.StatusFailed) {
			s.Remind.CancelSession(sid)
		}
	}
}

// deliverReminder sends a reminder unless the machine or session is muted
// or an app is in the foreground, which has the request in front of it.
func (s *Sender) deliverReminder(ctx context.Context, m Message) {
	if s.Hub.ForegroundSubscribers() > 0 || (s.Muted != nil && s.Muted()) {
		return
	}
	if m.Plaintext.SessionID != nil && s.Session != nil {
		if info, _ := s.Session(*m.Plaintext.SessionID); info.Muted {
			return
		}
	}
	s.Deliver(ctx, m)
}
