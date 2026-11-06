package push

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// deviceLive is what the Sender knows about one device's activity.
type deviceLive struct {
	token    string // update token the activity was last seen with
	started  bool   // a start went out and no update token has come back yet
	startDue bool   // send a start on the next change with something waiting
	lastAt   time.Time
	last     LiveState
	pending  *LiveState // an update held back by liveCoalesce
}

// observe feeds the tracker. Muted sessions are left out, so they never
// start or hold the activity.
func (s *Sender) observe(ev wire.Event, sid string, info SessionInfo, now time.Time) {
	switch pl := deref(ev.Payload).(type) {
	case wire.ApprovalOpenEvent:
		if info.Muted {
			return
		}
		a := pl.Approval
		bib, agent := a.Bib, a.Agent
		if bib == nil {
			bib = info.Bib
		}
		if agent == "" && info.Agent != nil {
			agent = *info.Agent
		}
		s.Live.Open(a.RequestID, a.CreatedAt, bib, agent, a.Subject, now)
	case wire.ApprovalClosedEvent:
		s.Live.Close(pl.RequestID, now)
	case wire.SessionEvent:
		sess, ok := sessionOf(pl.Session)
		if !ok {
			return
		}
		if sess.Muted {
			s.Live.Remove(sess.ID, now)
			return
		}
		reason := ""
		if sess.Exit != nil {
			reason = liveReason(sess.Exit.Reason)
		}
		s.Live.Session(sess.ID, sess.Status, sess.Bib, sess.Agent, reason, now)
	case wire.SessionRemovedEvent:
		if sid != "" {
			s.Live.Remove(sid, now)
		}
	}
}

// pumpLive asks the tracker what changed and pushes it to every iOS
// device: start through the start token to a device with no activity,
// update and end through the update token to a device with one. Held-back
// updates go out once their window has passed.
func (s *Sender) pumpLive(ctx context.Context) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	now := s.now()
	st, ev, ok := s.Live.Next(now)
	gen := s.Devices.Gen()
	if !ok && !s.anyPendingLocked() && gen == s.liveGen {
		return
	}
	list, err := s.Devices.List()
	if err != nil {
		s.logf("push: %v", err)
		return
	}
	s.liveGen = gen
	seen := map[string]bool{}
	hostID := ""
	host := func() string {
		if hostID == "" {
			if k, err := s.Keys(); err == nil {
				hostID = k.HostID
			}
		}
		return hostID
	}
	for _, d := range list {
		if d.Dead || d.EffectivePlatform() != wire.PlatformIOS {
			continue
		}
		seen[d.DeviceID] = true
		dl := s.liveDev[d.DeviceID]
		if dl == nil {
			dl = &deviceLive{}
			s.liveDev[d.DeviceID] = dl
		}
		fresh := d.LiveActivityToken != "" && d.LiveActivityToken != dl.token
		if fresh {
			dl.token, dl.started, dl.startDue = d.LiveActivityToken, false, false
		}
		if d.LiveActivityToken == "" {
			dl.token, dl.pending = "", nil
		}
		handled := false
		if ok {
			handled = s.liveEvent(ctx, d, dl, st, ev, host, now)
		}
		if fresh && !handled {
			if cur, shown := s.Live.Current(); shown {
				s.queueUpdate(ctx, d, dl, cur, now)
			}
		}
		if dl.pending != nil && dl.token != "" && now.Sub(dl.lastAt) >= liveCoalesce {
			s.sendLive(ctx, d, dl, liveUpdate, *dl.pending, nil, now)
		}
	}
	for id := range s.liveDev {
		if !seen[id] {
			delete(s.liveDev, id)
		}
	}
}

// liveEvent applies one tracker event to one device and reports whether
// anything was sent or queued. A start goes out on the tracker's start, or
// on the next change with something waiting when a start is due: after a
// failed start (retried once) or after the relay reported the update token
// gone.
func (s *Sender) liveEvent(ctx context.Context, d Device, dl *deviceLive, st LiveState, ev string, host func() string, now time.Time) bool {
	wantStart := ev == liveStart || (dl.startDue && ev == liveUpdate && st.Waiting > 0)
	switch {
	case dl.token != "" && ev == liveEnd:
		token := dl.token
		s.sendLive(ctx, d, dl, liveEnd, st, nil, now)
		if err := s.Devices.EndLiveActivity(d.DeviceID, token); err != nil {
			s.logf("push: %v", err)
		}
		*dl = deviceLive{}
		return true
	case dl.token != "":
		s.queueUpdate(ctx, d, dl, st, now)
		return true
	case wantStart && d.LiveActivityStartToken != "" && !dl.started && host() != "":
		retry := dl.startDue && ev != liveStart
		dl.startDue = false
		if err := s.sendLive(ctx, d, dl, liveStart, st, &liveAttributes{HostID: host()}, now); err != nil {
			dl.startDue = !retry
			return true
		}
		dl.started = true
		return true
	case ev == liveEnd:
		dl.started, dl.startDue = false, false
	}
	return false
}

// queueUpdate sends st now when the device's window has passed or the
// change involves needs_you, and otherwise holds it as the pending update.
// The pending state is kept unmasked; sendLive masks it for the device as
// it is when the update goes out.
func (s *Sender) queueUpdate(ctx context.Context, d Device, dl *deviceLive, st LiveState, now time.Time) {
	if dl.lastAt.IsZero() || now.Sub(dl.lastAt) >= liveCoalesce ||
		st.Status == wire.LevelNeedsYou || dl.last.Status == wire.LevelNeedsYou {
		s.sendLive(ctx, d, dl, liveUpdate, st, nil, now)
		return
	}
	dl.pending = &st
}

// sendLive posts one Live Activity push, masking st for d's current
// command setting. A 410 on the update token makes a start due.
func (s *Sender) sendLive(ctx context.Context, d Device, dl *deviceLive, ev string, st LiveState, attrs *liveAttributes, now time.Time) error {
	token := dl.token
	if ev == liveStart {
		token = d.LiveActivityStartToken
	}
	stale := now.Unix() + liveStale
	la := &liveActivity{Event: ev, ContentState: mask(st, d), Attributes: attrs, StaleDate: &stale}
	if ev == liveEnd {
		dismiss := now.Unix() + liveDismiss
		la.DismissalDate = &dismiss
	}
	dl.lastAt, dl.last, dl.pending = now, st, nil
	body := relayPush{Token: token, Env: d.Env, Kind: relayLiveActivity, LiveActivity: la}
	err := s.post(ctx, d, body)
	if err == nil {
		return nil
	}
	s.logf("push: device %s live activity %s: %v", idHash(d.DeviceID), ev, err)
	var st410 *errStatus
	if ev != liveStart && errors.As(err, &st410) && st410.code == http.StatusGone {
		dl.token, dl.startDue, dl.started = "", true, false
	}
	return err
}

func (s *Sender) anyPendingLocked() bool {
	for _, dl := range s.liveDev {
		if dl.pending != nil {
			return true
		}
	}
	return false
}

// liveReason is the exit reason as the Live Activity may carry it: one of
// a fixed vocabulary, else empty so the key is left out. The content state
// is not sealed, so no other text may reach it.
func liveReason(r string) string {
	switch r {
	case wire.ExitReasonExit, wire.ExitReasonInterrupted, wire.ExitReasonError, wire.ExitReasonUsageLimit:
		return r
	}
	return ""
}

// mask drops the command for a device that did not opt in to command text.
func mask(st LiveState, d Device) LiveState {
	if !d.LiveActivityCommand {
		st.Command = nil
	}
	return st
}

// sessionOf reads the session a session event carries.
func sessionOf(v any) (session.Session, bool) {
	switch s := v.(type) {
	case session.Session:
		return s, true
	case *session.Session:
		if s != nil {
			return *s, true
		}
	}
	return session.Session{}, false
}
