package push

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// liveStartConfirm is how long a needs_you waits, on a device sent a Live
// Activity start, for the activity's update token to prove it started.
// With no token by then the push goes to that device after all.
var liveStartConfirm = 10 * time.Second

// send delivers m. A device whose running Live Activity shows the event
// gets the activity update with an alert instead of the push; a device
// just sent a start (which alerts itself) gets the push only if the
// activity is not confirmed within liveStartConfirm. Every other device,
// and every Watch, gets the push; while a phone's activity stands in, the
// Watch's push is an alert, since there is no phone alert to mirror.
func (s *Sender) send(ctx context.Context, m Message) []Result {
	s.init()
	sid := ""
	if m.Plaintext.SessionID != nil {
		sid = *m.Plaintext.SessionID
	}
	shows := m.Kind == relayAlert && s.Live.Shows(m.Plaintext.Kind, m.Plaintext.RequestID, sid)
	alerted, started := s.pumpLiveAlert(ctx, shows)
	if len(alerted)+len(started) > 0 {
		m.watchAlert = true
	}
	res := s.deliver(ctx, m, func(d Device) bool { return !alerted[d.DeviceID] && started[d.DeviceID] == nil })
	for id := range alerted {
		if m.Plaintext.Kind == wire.PushNeedsYou {
			s.confirmRequest(m, id, nil)
		}
		res = append(res, Result{DeviceID: id})
	}
	for id, attempt := range started {
		if m.Plaintext.Kind == wire.PushNeedsYou {
			s.confirmRequest(m, id, attempt)
		} else {
			s.confirmStart(m, id, attempt)
		}
		res = append(res, Result{DeviceID: id})
	}
	if m.Plaintext.Kind == wire.PushNeedsYou && len(alerted)+len(started) > 0 {
		// No notification reaches those phones' service extension, so the request goes to
		// the app silently too: its widget, Siri count and Watch replies know it is waiting.
		bg := m
		bg.Kind = relayBackground
		s.deliver(ctx, bg, func(d Device) bool { return alerted[d.DeviceID] || started[d.DeviceID] != nil })
	}
	return res
}

// confirmStart sends m to device id after liveStartConfirm unless the
// activity the start made has registered its update token by then, or the
// request is no longer waiting.
func (s *Sender) confirmStart(m Message, id string, attempt *liveStartAttempt) {
	time.AfterFunc(liveStartConfirm, func() { s.startUnconfirmed(m, id, attempt) })
}

func (s *Sender) startUnconfirmed(m Message, id string, attempt *liveStartAttempt) {
	if m.Plaintext.Kind == wire.PushNeedsYou {
		s.liveMu.Lock()
		pending := s.deliveryAttempts[id+"|"+m.Plaintext.RequestID]
		s.liveMu.Unlock()
		if pending != nil {
			s.requestUnconfirmed(m, id, pending)
		}
		return
	}
	list, err := s.Devices.List()
	s.liveMu.Lock()
	dl := s.liveDev[id]
	if attempt == nil || dl == nil || dl.started != attempt {
		s.liveMu.Unlock()
		return
	}
	// Accepted by APNs is not confirmation that an activity started. Release this
	// attempt even if its request already closed, so a later cycle may start.
	dl.started = nil
	s.liveMu.Unlock()
	if err != nil {
		s.logf("push: could not confirm Live Activity: device state unreadable")
		return
	}
	for _, d := range list {
		if d.DeviceID == id && d.LiveActivityToken != "" {
			return
		}
	}
	if rid := m.Plaintext.RequestID; rid != "" && (!s.Live.Shows(wire.PushNeedsYou, rid, "") ||
		s.Remind != nil && s.Remind.Pending != nil && !s.Remind.Pending(rid)) {
		return
	}
	s.deliver(context.Background(), m, func(d Device) bool { return d.DeviceID == id })
}
