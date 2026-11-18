package push

import (
	"time"
)

// retryDelays are the waits before a needs_you that reached no device is
// sent again. A relay or network outage longer than one send's own tries
// would otherwise leave the request waiting with nobody told.
var retryDelays = []time.Duration{time.Minute, 3 * time.Minute, 9 * time.Minute}

// retryLater sends m again after the next delay while its request still
// waits, at most len(retryDelays) times. A send that reaches a device
// arms the usual reminder and ends the retries.
func (s *Sender) retryLater(m Message) {
	id := m.Plaintext.RequestID
	if id == "" || m.Plaintext.Reminder || s.Remind == nil {
		return
	}
	s.mu.Lock()
	n := s.retries[id]
	if n >= len(retryDelays) {
		delete(s.retries, id)
		s.mu.Unlock()
		s.logf("push: needs_you reached no device after %d retries, giving up", n)
		return
	}
	if s.retries == nil {
		s.retries = map[string]int{}
	}
	s.retries[id] = n + 1
	s.mu.Unlock()
	s.logf("push: needs_you reached no device, retry %d in %s", n+1, retryDelays[n])
	s.Remind.after(retryDelays[n], func() {
		if s.Remind.Pending == nil || !s.Remind.Pending(id) {
			s.forgetRetry(id)
			return
		}
		s.releaseHeld(m)
	})
}

func (s *Sender) forgetRetry(id string) {
	s.mu.Lock()
	delete(s.retries, id)
	s.mu.Unlock()
}
