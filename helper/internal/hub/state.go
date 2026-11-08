package hub

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/gettogo/togo/helper/internal/activity"
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
	// exitStatus is wire.StatusEnded or wire.StatusFailed while the
	// session's agent has exited, with exit saying how; both are cleared
	// by the next agent hook or a relaunch. The session's @togo_exit
	// option keeps them across a daemon restart: exitKnown is false until
	// the hub has read or written it, and the first listing seen then
	// supplies the stored exit.
	exitStatus string
	exit       *wire.SessionExit
	exitKnown  bool
	// exitRestored marks an exit read back or found at start, which is
	// never announced as new.
	exitRestored bool
	// awaitRun is set when the agent was started again and cleared by the
	// first hook of the new run, so a late exit hook from the run the
	// relaunch killed is not taken for the new one's.
	awaitRun bool
	// relaunchedAt is when the agent was last restarted in its pane.
	relaunchedAt time.Time
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

// SetExit records that the session's agent exited, status being
// wire.StatusEnded or wire.StatusFailed, and publishes the session. The
// status derived from hooks and output is dropped, so once the exit clears
// the session reads as tmux reports it until the agent says otherwise. An
// exit that fails validation, or any other status, is ignored.
func (h *Hub) SetExit(id string, status string, exit *wire.SessionExit) {
	if exit == nil || exit.Validate() != nil || (status != wire.StatusEnded && status != wire.StatusFailed) {
		return
	}
	e := *exit
	h.exitMu.Lock()
	h.mu.Lock()
	st := h.stateLocked(id)
	if st.awaitRun && !st.relaunchedAt.IsZero() && h.clock().Sub(st.relaunchedAt) < relaunchExitGrace {
		// The run a relaunch just killed reporting its own end.
		h.mu.Unlock()
		h.exitMu.Unlock()
		return
	}
	st.exitStatus, st.exit, st.exitKnown, st.exitRestored = status, &e, true, false
	st.status, st.activity = "", nil
	h.mu.Unlock()
	h.saveExit(id, &session.SavedExit{Status: status, Exit: e})
	h.exitMu.Unlock()
	h.publishSession(id)
}

// RestoreExit records an exit the daemon did not see happen: one stored
// before a restart, or an agent found gone when the daemon started. It is
// kept like SetExit's, but subscribers see it as restored, so it sends no
// push. A session the hub already holds an exit for is left alone.
func (h *Hub) RestoreExit(id string, status string, exit *wire.SessionExit) {
	if exit == nil || exit.Validate() != nil || (status != wire.StatusEnded && status != wire.StatusFailed) {
		return
	}
	e := *exit
	h.exitMu.Lock()
	h.mu.Lock()
	st := h.stateLocked(id)
	if st.exitStatus != "" {
		h.mu.Unlock()
		h.exitMu.Unlock()
		return
	}
	st.exitStatus, st.exit, st.exitKnown, st.exitRestored = status, &e, true, true
	st.status, st.activity = "", nil
	h.mu.Unlock()
	h.saveExit(id, &session.SavedExit{Status: status, Exit: e})
	h.exitMu.Unlock()
	h.publishSession(id)
}

// ClearExit returns the session to its derived status and publishes it
// when it was ended or failed. Hooks call it when the agent shows it is
// running, which also marks the current run as started.
func (h *Hub) ClearExit(id string) {
	h.exitMu.Lock()
	h.mu.Lock()
	st := h.stateLocked(id)
	was := st.exitStatus != ""
	// Before the hub has read the session's stored exit, there may be one
	// to remove.
	stored := was || !st.exitKnown
	st.exitStatus, st.exit, st.exitKnown, st.exitRestored = "", nil, true, false
	st.awaitRun = false
	h.mu.Unlock()
	if stored {
		h.saveExit(id, nil)
	}
	h.exitMu.Unlock()
	if was {
		h.publishSession(id)
	}
}

// saveExit writes the session's exit to its @togo_exit option, or removes
// it for nil. Callers hold exitMu, so the option ends up matching the
// hub's last change. A failed write costs only the restart case: the
// session's exit is still correct while this daemon runs.
func (h *Hub) saveExit(id string, e *session.SavedExit) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	if err := h.svc.SaveExit(ctx, id, e); err != nil && h.svc.Logf != nil {
		h.svc.Logf("hub: session %s: keeping the exit: %v", id, err)
	}
}

// HookExitAllowed reports whether an exit reported by an agent hook can
// belong to the session's current run: false after a relaunch until the
// new run's first hook, since the run the relaunch killed may still report
// its own end.
func (h *Hub) HookExitAllowed(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.sessions[id]
	return !ok || !st.awaitRun
}

// Ended reports whether the session's agent has exited: its status is
// ended or failed until a hook or a relaunch clears it.
func (h *Hub) Ended(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.sessions[id]
	return ok && st.exitStatus != ""
}

// Exit returns the session's exit status and exit, or "" and nil.
func (h *Hub) Exit(id string) (string, *wire.SessionExit) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.sessions[id]
	if !ok || st.exit == nil {
		return "", nil
	}
	e := *st.exit
	return st.exitStatus, &e
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

// RecordOutput notes that the session just produced output, for its
// activity history.
func (h *Hub) RecordOutput(id string) { h.activity.Record(id, h.clock()) }

// RunActivity republishes the sessions whose activity history moved each
// time a bucket rolls over, while an app is in the foreground to see it, so
// the bars keep moving while a session works and fall away after it stops.
func (h *Hub) RunActivity(ctx context.Context) {
	for {
		now := h.clock()
		next := now.Truncate(activity.BucketSpan).Add(activity.BucketSpan)
		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Sub(now)):
		}
		h.publishRolling(h.clock())
	}
}

func (h *Hub) publishRolling(now time.Time) {
	ids := h.activity.Rolling(now)
	if h.ForegroundSubscribers() == 0 {
		return
	}
	for _, id := range ids {
		h.publishSession(id)
	}
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
		if !st.exitKnown {
			st.exitKnown = true
			if e := s.SavedExit; e != nil {
				exit := e.Exit
				st.exitStatus, st.exit, st.exitRestored = e.Status, &exit, true
			}
		}
		if st.status != "" {
			s.Status, s.Activity = st.status, st.activity
		}
		if st.exitStatus != "" {
			exit := *st.exit
			s.Status, s.Activity, s.Exit = st.exitStatus, nil, &exit
			s.ExitRestored = st.exitRestored
		}
		if waiting[s.ID] {
			s.Status, s.Exit = statusNeedsYou, nil
		}
		s.Meta = st.meta
		s.ActivityHistory = h.activity.History(s.ID, h.clock())
		s.Mission = nil
		if h.mission != nil {
			if m, ok := h.mission(s.ID); ok {
				s.Mission = &m
			}
		}
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
		h.activity.Forget(*ev.SessionID)
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

// SetMission installs the reader for Session.mission. Call it before the
// hub publishes; the reader must not call back into the hub.
func (h *Hub) SetMission(fn func(id string) (wire.MissionSummary, bool)) {
	h.mu.Lock()
	h.mission = fn
	h.mu.Unlock()
}

// MissionChanged publishes the session again because its harness summary
// changed. The harness engine debounces calls to once a second per session.
func (h *Hub) MissionChanged(id string) {
	go h.publishSession(id)
}

// Status is the status hooks and output last set for a session, or "" when
// none was set.
func (h *Hub) Status(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st, ok := h.sessions[id]; ok {
		return st.status
	}
	return ""
}
