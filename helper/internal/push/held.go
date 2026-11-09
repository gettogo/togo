package push

import (
	"context"
	"sync"
	"time"
)

// foregroundRecheck is when a needs_you held back because an app was in
// the foreground is looked at again: just after the hub's 45 s foreground
// lease (hub.ForegroundLease) would have run out with no request since.
var foregroundRecheck = 46 * time.Second

// heldPushes keeps needs_you pushes suppressed only because an app looked
// foregrounded. A phone that drops off (a lift, a Wi-Fi hand-off, the app
// killed without closing SSH) keeps its lease for up to 45 s, and the
// push was never re-evaluated: the agent then waited with no notification.
type heldPushes struct {
	mu     sync.Mutex
	timers map[string]*time.Timer // request id -> its recheck
}

// hold rechecks m after d: still pending and nothing in the foreground
// means it goes out then, as the first push for its request.
func (h *heldPushes) hold(m Message, d time.Duration, fire func(Message)) {
	rid := m.Plaintext.RequestID
	if rid == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.timers == nil {
		h.timers = map[string]*time.Timer{}
	}
	if _, ok := h.timers[rid]; ok {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(d, func() {
		h.mu.Lock()
		if h.timers[rid] != t {
			h.mu.Unlock()
			return
		}
		delete(h.timers, rid)
		h.mu.Unlock()
		fire(m)
	})
	h.timers[rid] = t
}

func (h *heldPushes) cancel(rid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t := h.timers[rid]; t != nil {
		t.Stop()
		delete(h.timers, rid)
	}
}

// releaseHeld sends a held needs_you if its request still waits and no
// app is in the foreground now; the normal reminder follows it.
func (s *Sender) releaseHeld(m Message) {
	if s.Remind == nil || s.Remind.Pending == nil || !s.Remind.Pending(m.Plaintext.RequestID) {
		return
	}
	if s.Hub.ForegroundSubscribers() > 0 || (s.Muted != nil && s.Muted()) {
		return
	}
	if m.Plaintext.SessionID != nil && s.Session != nil {
		if info, _ := s.Session(*m.Plaintext.SessionID); info.Muted {
			return
		}
	}
	m.Plaintext.TS = s.now().UnixMilli()
	s.afterDeliver(m, s.send(context.Background(), m), "")
}
