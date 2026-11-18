package push

import "time"

// liveRetryMax is how many updates in a row may fail before the state is
// left for the next change to replace.
const liveRetryMax = 5

// liveRetryWait is the extra wait before the update after n failed ones. It
// keeps a paused or unreachable relay from being asked every tick.
func liveRetryWait(n int) time.Duration {
	switch {
	case n <= 0:
		return 0
	case n == 1:
		return 5 * time.Second
	case n == 2:
		return 15 * time.Second
	case n == 3:
		return 45 * time.Second
	}
	return 2 * time.Minute
}

// retryLive keeps an update that did not reach the relay as the device's
// pending one, so the Lock Screen is not left on the state before it. The
// caller holds liveMu.
func (s *Sender) retryLive(d Device, dl *deviceLive, st LiveState) {
	dl.fails++
	if dl.fails > liveRetryMax {
		s.logf("push: device %s live activity update given up after %d tries", idHash(d.DeviceID), liveRetryMax)
		dl.fails = 0
		return
	}
	dl.pending = &st
}

// ResendLive queues the current state for every running activity. The
// relay's will replaces it with "offline" on the devices while the tracker
// still believes its own state is shown.
func (s *Sender) ResendLive() {
	s.init()
	cur, shown := s.Live.Current()
	if !shown {
		return
	}
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	for _, dl := range s.liveDev {
		if dl.token == "" {
			continue
		}
		st := cur
		dl.pending, dl.lastAt, dl.fails = &st, time.Time{}, 0
	}
}
