package push

import (
	"context"
	"errors"
	"github.com/gettogo/togo/helper/internal/approval"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
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
	alert    bool       // the next update carries an alert, standing in for a push
	alerted  bool       // an update with an alert went out this pump
}

// observe feeds the tracker. Muted sessions, and every session while the
// machine is muted, are left out, so they never start, update or hold the
// activity; a session muted while it waits takes its requests with it.
func (s *Sender) observe(ev wire.Event, sid string, info SessionInfo, now time.Time) {
	machineMuted := s.Muted != nil && s.Muted()
	switch pl := deref(ev.Payload).(type) {
	case wire.ApprovalOpenEvent:
		if info.Muted || machineMuted {
			if info.Muted && sid != "" {
				s.Live.Remove(sid, now)
			}
			return
		}
		a := pl.Approval
		bib, agent := a.Bib, requestAgent(a.Agent, info)
		if bib == nil {
			bib = info.Bib
		}
		owner := sid
		if a.SessionID != nil {
			owner = *a.SessionID
		}
		c := requestContext(a)
		c.Title = sessionTitle(info.Name)
		s.Live.OpenRequest(a.RequestID, a.CreatedAt, bib, agent, a.Subject, owner, c, now)
	case wire.ApprovalClosedEvent:
		s.Live.Close(pl.RequestID, now)
	case wire.SessionEvent:
		sess, ok := sessionOf(pl.Session)
		if !ok {
			return
		}
		if sess.Muted || machineMuted {
			s.Live.Remove(sess.ID, now)
			return
		}
		s.Live.Describe(sess.ID, sessionTitle(sess.Name), stepText(sess.Activity))
		reason := ""
		if sess.Exit != nil {
			reason = liveReason(sess.Exit.Reason)
		}
		s.Live.Session(sess.ID, sess.Status, sess.Bib, agents.Resolve(sess.Agent, sess.Command), reason, now)
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
	s.pumpLiveAlert(ctx, false)
}

// pumpLiveAlert is pumpLive for a push the activity shows (alert): each
// device with a running activity gets the update at once with an alert,
// the current state again if nothing changed, in place of the push.
// alerted holds those devices; started holds devices sent a start, whose
// own alert stands in for the push until the activity is confirmed.
func (s *Sender) pumpLiveAlert(ctx context.Context, alert bool) (alerted, started map[string]bool) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	now := s.now()
	st, ev, ok := s.Live.Next(now)
	gen := s.Devices.Gen()
	if !ok && !alert && !s.anyPendingLocked() && gen == s.liveGen {
		return nil, nil
	}
	list, err := s.Devices.List()
	if err != nil {
		s.logf("push: %v", err)
		return nil, nil
	}
	alerted, started = map[string]bool{}, map[string]bool{}
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
		dl.alert, dl.alerted = alert && dl.token != "", false
		wasStarted := dl.started
		handled := false
		if ok {
			handled = s.liveEvent(ctx, d, dl, st, ev, host, now)
		}
		if fresh && !handled {
			if cur, shown := s.Live.Current(); shown {
				s.queueUpdate(ctx, d, dl, cur, now)
			}
		}
		if dl.alert && dl.token != "" {
			// Nothing changed for this device: the current state again, with the alert.
			if cur, shown := s.Live.Current(); shown {
				s.sendLive(ctx, d, dl, liveUpdate, cur, nil, now)
			}
		}
		if dl.pending != nil && dl.token != "" && now.Sub(dl.lastAt) >= holdFor(dl.last, *dl.pending) {
			s.sendLive(ctx, d, dl, liveUpdate, *dl.pending, nil, now)
		}
		if dl.alerted {
			alerted[d.DeviceID] = true
		}
		if alert && dl.started && !wasStarted {
			started[d.DeviceID] = true
		}
		dl.alert, dl.alerted = false, false
	}
	for id := range s.liveDev {
		if !seen[id] {
			delete(s.liveDev, id)
		}
	}
	return alerted, started
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
	if dl.alert || dl.lastAt.IsZero() || now.Sub(dl.lastAt) >= holdFor(dl.last, st) ||
		(st.Status == wire.LevelNeedsYou || dl.last.Status == wire.LevelNeedsYou) && !onlyStep(dl.last, st) {
		s.sendLive(ctx, d, dl, liveUpdate, st, nil, now)
		return
	}
	dl.pending = &st
}

// holdFor is how long after the last update next waits: longer when only
// the step changed, so a busy agent does not spend the push budget.
func holdFor(last, next LiveState) time.Duration {
	if last.Status == next.Status && onlyStep(last, next) {
		return liveStepCoalesce
	}
	return liveCoalesce
}

// sendLive posts one Live Activity push, masking st for d's current
// command setting and sealing its context for d. A token APNs refused on the update
// token makes a start due.
func (s *Sender) sendLive(ctx context.Context, d Device, dl *deviceLive, ev string, st LiveState, attrs *liveAttributes, now time.Time) error {
	token := dl.token
	if ev == liveStart {
		token = d.LiveActivityStartToken
	}
	stale := now.Unix() + liveStale
	if st.Status == wire.LevelNeedsYou && st.Since > 0 {
		// The buttons go with the answer window: past it the helper refuses the answer,
		// and an asleep Mac sends no update to say so.
		stale = min(stale, st.Since+int64(approval.DefaultTTL/time.Second))
	}
	cs := mask(st, d)
	cs.Ctx = s.sealContext(d, st.context)
	alert := dl.alert && ev == liveUpdate
	// A done the user keeps silent still stands in for its push, but neither lights the
	// screen nor sounds, as that push's notification would be passive.
	quiet := alert && st.Status == wire.PushDone && d.DoneSilent
	la := &liveActivity{Event: ev, ContentState: cs, Attributes: attrs, StaleDate: &stale,
		Urgent: ev == liveUpdate && st.Waiting < dl.last.Waiting, Alert: alert && !quiet}
	if ev == liveEnd {
		dismiss := now.Unix() + liveDismiss
		la.DismissalDate = &dismiss
	}
	dl.lastAt, dl.last, dl.pending = now, st, nil
	body := relayPush{Token: token, Env: d.Env, Kind: relayLiveActivity, LiveActivity: la}
	err := s.post(ctx, d, body)
	if alert {
		dl.alert, dl.alerted = false, err == nil
	}
	if err == nil {
		return nil
	}
	s.logf("push: device %s live activity %s: %v", idHash(d.DeviceID), ev, err)
	var refused *errStatus
	if ev != liveStart && errors.As(err, &refused) && refused.deadToken {
		dl.token, dl.startDue, dl.started = "", true, false
	}
	return err
}

// sealContext seals c for d, or nil when there is nothing to show or it
// cannot be sealed: the activity then shows its plain fields.
func (s *Sender) sealContext(d Device, c LiveContext) *string {
	if c.empty() {
		return nil
	}
	var pub [32]byte
	if !decode32(d.X25519, &pub) {
		return nil
	}
	keys, err := s.Keys()
	if err != nil {
		return nil
	}
	ct := sealLiveContext(pub, c, []byte(keys.HostID+"|"+d.DeviceID))
	if ct == "" {
		return nil
	}
	return &ct
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

// mask drops the plain command field for every device.
func mask(st LiveState, d Device) LiveState {
	// Command text never goes in plain: APNs and the relay would see it.
	// The sealed context's subject carries it (redacted) to every device.
	st.Command = nil
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
