package push

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// One bounded confirmation per device/request, independent of activity token arrival.
// A token proves the activity exists, not that the renderer has its request data.
type liveDeliveryAttempt struct{ start *liveStartAttempt }

func (s *Sender) confirmRequest(m Message, id string, start *liveStartAttempt) {
	key := id + "|" + m.Plaintext.RequestID
	s.liveMu.Lock()
	if s.deliveryAttempts[key] != nil {
		s.liveMu.Unlock()
		return
	}
	attempt := &liveDeliveryAttempt{start: start}
	s.deliveryAttempts[key] = attempt
	s.liveMu.Unlock()
	time.AfterFunc(liveStartConfirm, func() { s.requestUnconfirmed(m, id, attempt) })
}

func (s *Sender) requestUnconfirmed(m Message, id string, attempt *liveDeliveryAttempt) {
	key := id + "|" + m.Plaintext.RequestID
	s.liveMu.Lock()
	if s.deliveryAttempts[key] != attempt {
		s.liveMu.Unlock()
		return
	}
	delete(s.deliveryAttempts, key)
	if dl := s.liveDev[id]; dl != nil && attempt.start != nil && dl.started == attempt.start {
		dl.started = nil
	}
	s.liveMu.Unlock()
	rid := m.Plaintext.RequestID
	if !s.Live.Shows(wire.PushNeedsYou, rid, "") || s.Remind != nil && s.Remind.Pending != nil && !s.Remind.Pending(rid) {
		return
	}
	devices, err := s.Devices.List()
	if err != nil {
		s.logf("push: could not confirm request data: device state unreadable")
		return
	}
	for _, d := range devices {
		if d.DeviceID == id && d.LiveActivityToken != "" && d.ContextRequestID == rid {
			return
		}
	}
	s.deliver(context.Background(), m, func(d Device) bool { return d.DeviceID == id })
}
