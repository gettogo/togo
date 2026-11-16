package push

import (
	"context"

	"github.com/gettogo/togo/helper/internal/wire"
)

// AnswerReceipt tells the device that sent an answer through the relay
// what became of it. The relay only says it queued the answer; without
// this the phone shows an approval the machine never applied.
//
// It goes to that device alone, and only if it said it understands the
// push when it paired. An answer that did not land is an alert, since the
// user believes otherwise; one that landed, or that someone else gave
// first, updates the app without a sound.
func (s *Sender) AnswerReceipt(ctx context.Context, deviceID, requestID, result string) {
	s.init()
	devices, err := s.Devices.List()
	if err != nil {
		s.logf("push: %v", err)
		return
	}
	for _, d := range devices {
		if d.DeviceID != deviceID {
			continue
		}
		if d.Dead || !d.AnswerReceipts {
			return
		}
		keys, err := s.Keys()
		if err != nil {
			s.logf("push: %v", err)
			return
		}
		m := Message{Kind: relayAlert, CollapseID: requestID, Plaintext: wire.PushPlaintext{
			V: 1, Kind: wire.PushAnswer, RequestID: requestID, Result: result, TS: s.now().UnixMilli(),
		}}
		if result == wire.AnswerApplied || result == wire.ConflictAnswered {
			m.Kind = relayBackground
		}
		if err := s.deliverOne(ctx, keys.HostID, d, m); err != nil {
			s.logf("push: device %s answer receipt: %v", idHash(d.DeviceID), err)
		}
		return
	}
}
