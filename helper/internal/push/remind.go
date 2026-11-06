package push

import (
	"context"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Reminders sends one reminder for a needs_you push whose request is still
// unanswered after a delay. Timers live in memory only: a restarted daemon
// reminds about nothing.
type Reminders struct {
	// After is the delay from the first delivered push; zero or less means
	// no reminder.
	After func() time.Duration
	// Pending reports whether the request is still waiting for an answer.
	Pending func(requestID string) bool
	// Deliver sends the reminder.
	Deliver func(context.Context, Message)
	// Now stamps the reminder; nil means time.Now.
	Now func() time.Time
	// AfterFunc runs fn after d and returns a function that stops it; nil
	// means time.AfterFunc.
	AfterFunc func(d time.Duration, fn func()) func() bool

	mu     sync.Mutex
	timers map[string]*reminder // request id -> its timer
	fired  map[string]string    // request id -> session, for requests already reminded
}

type reminder struct {
	id      string
	session string
	msg     Message
	opened  time.Time
	stop    func() bool
}

func (r *Reminders) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// arm starts t's timer for d. The caller holds r.mu.
func (r *Reminders) arm(t *reminder, d time.Duration) {
	after := r.AfterFunc
	if after == nil {
		after = func(d time.Duration, fn func()) func() bool { return time.AfterFunc(d, fn).Stop }
	}
	t.stop = after(d, func() { r.fire(t) })
}

// Schedule arranges the one reminder for m, a needs_you push that went out.
// A request already scheduled, or already reminded, gets no second one.
func (r *Reminders) Schedule(m Message) {
	p := m.Plaintext
	if p.Kind != wire.PushNeedsYou || p.RequestID == "" || p.Reminder || r.After == nil {
		return
	}
	d := r.After()
	if d <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.timers[p.RequestID]; ok {
		return
	}
	if _, ok := r.fired[p.RequestID]; ok {
		return
	}
	if r.timers == nil {
		r.timers = map[string]*reminder{}
	}
	t := &reminder{id: p.RequestID, msg: m, opened: r.now()}
	if p.SessionID != nil {
		t.session = *p.SessionID
	}
	r.arm(t, d)
	r.timers[p.RequestID] = t
}

// Reschedule applies a changed delay to the armed reminders: off drops
// them, a new value counts from when each request was first pushed.
func (r *Reminders) Reschedule() {
	d := time.Duration(0)
	if r.After != nil {
		d = r.After()
	}
	r.mu.Lock()
	var stops []func() bool
	for id, t := range r.timers {
		stops = append(stops, t.stop)
		if d <= 0 {
			delete(r.timers, id)
			continue
		}
		r.arm(t, max(d-r.now().Sub(t.opened), 0))
	}
	r.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

// Cancel drops the reminder for a request, if any, and forgets it.
func (r *Reminders) Cancel(requestID string) {
	r.mu.Lock()
	t := r.timers[requestID]
	delete(r.timers, requestID)
	delete(r.fired, requestID)
	r.mu.Unlock()
	if t != nil {
		t.stop()
	}
}

// CancelSession drops every reminder for a session that ended or went away.
func (r *Reminders) CancelSession(sessionID string) {
	r.mu.Lock()
	var stops []func() bool
	for id, t := range r.timers {
		if t.session == sessionID {
			stops = append(stops, t.stop)
			delete(r.timers, id)
		}
	}
	for id, sid := range r.fired {
		if sid == sessionID {
			delete(r.fired, id)
		}
	}
	r.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

func (r *Reminders) fire(t *reminder) {
	r.mu.Lock()
	if r.timers[t.id] != t {
		r.mu.Unlock()
		return
	}
	delete(r.timers, t.id)
	if r.fired == nil {
		r.fired = map[string]string{}
	}
	r.fired[t.id] = t.session
	r.mu.Unlock()
	if r.Pending == nil || !r.Pending(t.id) || r.Deliver == nil {
		return
	}
	m := t.msg
	m.Plaintext.Reminder = true
	m.Plaintext.TS = r.now().UnixMilli()
	r.Deliver(context.Background(), m)
}
