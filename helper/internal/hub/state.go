package hub

import (
	"cmp"
	"context"
	"slices"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// statusNeedsYou overrides every other status while an approval is pending.
const statusNeedsYou = "needs_you"

// sessionState is what the hub knows about a session beyond tmux.
type sessionState struct {
	status   string
	activity *string
	meta     wire.SessionMeta
	// muted mirrors the session's tmux option; known is false until it
	// has been read or set.
	muted bool
	known bool
}

func (h *Hub) stateLocked(id string) *sessionState {
	st, ok := h.sessions[id]
	if !ok {
		st = &sessionState{}
		h.sessions[id] = st
	}
	return st
}

// SetStatus records the status and activity derived from hooks and output,
// then publishes the updated session. An empty status clears it, so the
// tmux-derived value shows again.
func (h *Hub) SetStatus(id, status string, activity *string) {
	h.mu.Lock()
	st := h.stateLocked(id)
	st.status, st.activity = status, activity
	h.mu.Unlock()
	h.publishSession(id)
}

// SetMeta records what an agent reported about a session and publishes the
// updated session.
func (h *Hub) SetMeta(id string, m wire.SessionMeta) {
	h.mu.Lock()
	h.stateLocked(id).meta = m
	h.mu.Unlock()
	h.publishSession(id)
}

// Muted reports whether pushes for the session are suppressed. The first
// call for a session the hub has not seen reads its tmux option.
func (h *Hub) Muted(id string) bool {
	h.mu.Lock()
	st, ok := h.sessions[id]
	if ok && st.known {
		muted := st.muted
		h.mu.Unlock()
		return muted
	}
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	sess, err := h.svc.Resolve(ctx, id)
	if err != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st = h.stateLocked(id)
	// A setMute that finished during the lookup is newer than this read.
	if !st.known {
		st.muted, st.known = sess.Muted, true
	}
	return st.muted
}

// Decorate fills status, activity, muted and meta from the hub's state.
func (h *Hub) Decorate(sessions []session.Session) []session.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.decorateLocked(sessions)
}

func (h *Hub) decorateLocked(sessions []session.Session) []session.Session {
	waiting := map[string]bool{}
	for _, a := range h.approvals {
		if a.SessionID != nil {
			waiting[*a.SessionID] = true
		}
	}
	out := append(make([]session.Session, 0, len(sessions)), sessions...)
	for i := range out {
		s := &out[i]
		if !s.Managed {
			continue
		}
		st := h.stateLocked(s.ID)
		// After the first read the hub's own writes are authoritative: a
		// listing that raced a setMute must not undo it.
		if st.known {
			s.Muted = st.muted
		} else {
			st.muted, st.known = s.Muted, true
		}
		if st.status != "" {
			s.Status, s.Activity = st.status, st.activity
		}
		if waiting[s.ID] {
			s.Status = statusNeedsYou
		}
		s.Meta = st.meta
	}
	return out
}

// pendingLocked returns the open approvals, oldest first.
func (h *Hub) pendingLocked() []wire.Approval {
	out := make([]wire.Approval, 0, len(h.approvals))
	for _, a := range h.approvals {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b wire.Approval) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.RequestID, b.RequestID))
	})
	return out
}

// trackLocked keeps the pending approvals in step with the events that open
// and close them, so snapshots and decoration need no approval store.
func (h *Hub) trackLocked(ev wire.Event) {
	switch p := ev.Payload.(type) {
	case wire.ApprovalOpenEvent:
		h.approvals[p.Approval.RequestID] = p.Approval
	case *wire.ApprovalOpenEvent:
		h.approvals[p.Approval.RequestID] = p.Approval
	case wire.ApprovalClosedEvent:
		delete(h.approvals, p.RequestID)
	case *wire.ApprovalClosedEvent:
		delete(h.approvals, p.RequestID)
	}
	if ev.Kind == wire.EventSessionRemoved && ev.SessionID != nil {
		delete(h.sessions, *ev.SessionID)
	}
}

// publishSession looks the session up and publishes it decorated. A session
// that no longer exists publishes nothing; its removal is announced by
// whoever saw it go.
func (h *Hub) publishSession(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	sess, err := h.svc.Resolve(ctx, id)
	if err != nil {
		return
	}
	h.PublishSession(sess)
}

// PublishSession decorates sess, publishes it as a session event and
// returns the decorated session.
func (h *Hub) PublishSession(sess session.Session) session.Session {
	var decorated session.Session
	id := sess.ID
	h.emit(wire.EventSession, &id, func() any {
		decorated = h.decorateLocked([]session.Session{sess})[0]
		return wire.SessionEvent{Session: decorated}
	})
	return decorated
}

// setMuted writes the mute flag to tmux and publishes the result. muteMu
// keeps the tmux write and the cache update in the same order across
// concurrent calls.
func (h *Hub) setMuted(ctx context.Context, id string, muted bool) (session.Session, error) {
	h.muteMu.Lock()
	defer h.muteMu.Unlock()
	sess, err := h.svc.SetMuted(ctx, id, muted)
	if err != nil {
		return session.Session{}, err
	}
	h.mu.Lock()
	st := h.stateLocked(id)
	st.muted, st.known = muted, true
	h.mu.Unlock()
	return h.PublishSession(sess), nil
}
