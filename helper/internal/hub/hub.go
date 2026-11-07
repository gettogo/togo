// Package hub numbers helper events, fans them out to subscribed
// connections and keeps the per-session state (status, activity, agent
// meta, mute, pending approvals) that decorates the Session objects the
// app sees.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/activity"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// queueSize bounds the events waiting for one subscriber. A subscriber that
// falls this far behind loses its backlog instead of slowing the hub.
const queueSize = 256

// lookupTimeout caps the tmux lookups behind calls that carry no context.
const lookupTimeout = 5 * time.Second

// Hub implements wire.Publisher. The zero value is not usable; call New.
type Hub struct {
	svc   *session.Service
	clock func() time.Time

	// pubMu serializes publishing end to end, so in-process consumers see
	// events in seq order.
	pubMu sync.Mutex

	// muteMu orders concurrent session.setMute calls.
	muteMu sync.Mutex
	// exitMu keeps each session's @togo_exit option in the order of the
	// exit changes it records.
	exitMu sync.Mutex
	// relaunching holds a *sync.Mutex per session id, taken around a whole
	// relaunch.
	relaunching sync.Map

	mu        sync.Mutex
	seq       int64
	subs      map[*rpc.Conn]*subscriber
	consumers []func(wire.Event)
	sessions  map[string]*sessionState
	activity  *activity.Tracker
	approvals map[string]wire.Approval
	// mission reads a session's harness summary (Session.mission); nil
	// leaves it out.
	mission func(id string) (wire.MissionSummary, bool)
}

// New returns a Hub over svc. clock stamps events; nil means time.Now.
func New(svc *session.Service, clock func() time.Time) *Hub {
	if clock == nil {
		clock = time.Now
	}
	return &Hub{
		svc:       svc,
		clock:     clock,
		subs:      map[*rpc.Conn]*subscriber{},
		sessions:  map[string]*sessionState{},
		activity:  activity.New(),
		approvals: map[string]wire.Approval{},
	}
}

// OnEvent registers fn to receive every later event, in seq order, on the
// publishing goroutine. fn holds up all publishing while it runs, so slow
// work such as a relay call must be handed off; fn must not call Publish.
func (h *Hub) OnEvent(fn func(wire.Event)) {
	h.mu.Lock()
	h.consumers = append(h.consumers, fn)
	h.mu.Unlock()
}

// Publish assigns the next seq and delivers the event to every subscriber
// and consumer. It never waits on a subscriber's connection.
func (h *Hub) Publish(kind string, sessionID *string, payload any) {
	h.emit(kind, sessionID, func() any { return payload })
}

// emit builds the payload and numbers the event in one critical section, so
// an event with a higher seq never carries older state than a lower one.
func (h *Hub) emit(kind string, sessionID *string, build func() any) {
	h.pubMu.Lock()
	defer h.pubMu.Unlock()

	h.mu.Lock()
	h.seq++
	ev := wire.Event{Seq: h.seq, TS: wire.Stamp(h.clock()), Kind: kind, SessionID: sessionID, Payload: build()}
	h.trackLocked(ev)
	for _, s := range h.subs {
		s.enqueue(ev)
	}
	consumers := h.consumers
	h.mu.Unlock()

	for _, fn := range consumers {
		fn(ev)
	}
}

// ForegroundSubscribers counts subscribers whose app is in the foreground
// and that sent a request within ForegroundLease, which is when pushes are
// unnecessary.
func (h *Hub) ForegroundSubscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clock()
	n := 0
	for _, s := range h.subs {
		if s.foreground && now.Sub(s.lastRequest) < ForegroundLease {
			n++
		}
	}
	return n
}

// Snapshot is the events.subscribe result. Its events follow it on the
// connection, starting after Seq.
type Snapshot struct {
	Seq       int64
	Sessions  []session.Session
	Approvals []wire.Approval
	// sent opens the subscriber's event stream.
	sent func()
}

// MarshalJSON encodes the result and opens the event stream. rpc encodes a
// response while holding the connection's write lock, which Notify also
// takes, so no event can reach the wire ahead of this response. A snapshot
// too large for a frame still opens the stream, though rpc then answers
// with an internal error; the client resubscribes either way.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	if s.sent != nil {
		s.sent()
	}
	sessions, approvals := s.Sessions, s.Approvals
	if sessions == nil {
		sessions = []session.Session{}
	}
	if approvals == nil {
		approvals = []wire.Approval{}
	}
	return json.Marshal(struct {
		Seq       int64             `json:"seq"`
		Sessions  []session.Session `json:"sessions"`
		Approvals []wire.Approval   `json:"approvals"`
	}{s.Seq, sessions, approvals})
}

// Subscribe registers conn for every event after the returned snapshot,
// replacing an earlier subscription on the same conn. Events are written
// only once the snapshot itself has been encoded for the response.
//
// The subscriber is registered and seq taken before tmux is listed, with no
// lock held during the list, so a slow tmux never holds up Publish. Events
// numbered after seq may already be reflected in the snapshot; they carry
// whole objects, so applying them again leaves the client at current state.
func (h *Hub) Subscribe(ctx context.Context, conn *rpc.Conn, foreground bool) (Snapshot, error) {
	if conn == nil {
		return Snapshot{}, errors.New("events.subscribe needs a served connection")
	}
	h.mu.Lock()
	prev := h.subs[conn]
	if prev != nil {
		prev.stop()
	}
	s := newSubscriber(conn, foreground, prev)
	s.lastRequest = h.clock()
	h.subs[conn] = s
	seq := h.seq
	h.mu.Unlock()
	go s.run(h)

	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	sessions, err := h.svc.List(ctx)
	if err != nil {
		s.stop()
		h.remove(s)
		return Snapshot{}, fmt.Errorf("snapshot sessions: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return Snapshot{
		Seq:       seq,
		Sessions:  h.decorateLocked(sessions),
		Approvals: h.pendingLocked(),
		sent:      s.open,
	}, nil
}

func (h *Hub) remove(s *subscriber) {
	h.mu.Lock()
	if h.subs[s.conn] == s {
		delete(h.subs, s.conn)
	}
	h.mu.Unlock()
}

// subscriber owns one connection's queue and the goroutine that drains it.
type subscriber struct {
	conn       *rpc.Conn
	foreground bool
	// lastRequest is guarded by Hub.mu.
	lastRequest time.Time
	queue       chan wire.Event
	// prev is the subscription this one replaced; its writer must finish
	// before this one starts, so the two never interleave.
	prev *subscriber

	opened   chan struct{}
	open     func()
	stopped  chan struct{}
	stop     func()
	finished chan struct{}
}

func newSubscriber(conn *rpc.Conn, foreground bool, prev *subscriber) *subscriber {
	s := &subscriber{
		conn:       conn,
		foreground: foreground,
		queue:      make(chan wire.Event, queueSize),
		prev:       prev,
		opened:     make(chan struct{}),
		stopped:    make(chan struct{}),
		finished:   make(chan struct{}),
	}
	s.open = sync.OnceFunc(func() { close(s.opened) })
	s.stop = sync.OnceFunc(func() { close(s.stopped) })
	return s
}

// enqueue never blocks. On overflow the backlog is discarded and the event
// dropped, so the client sees a seq gap and resubscribes.
func (s *subscriber) enqueue(ev wire.Event) {
	select {
	case s.queue <- ev:
		return
	default:
	}
	for {
		select {
		case <-s.queue:
		default:
			return
		}
	}
}

func (s *subscriber) run(h *Hub) {
	defer close(s.finished)
	defer h.remove(s)
	if s.prev != nil {
		<-s.prev.finished
		s.prev = nil
	}
	select {
	case <-s.opened:
	case <-s.stopped:
		return
	case <-s.conn.Done():
		return
	}
	for {
		select {
		case ev := <-s.queue:
			err := s.conn.Notify(wire.NotificationMethod, ev)
			// An event too large for a frame is skipped; the gap it
			// leaves makes the client resubscribe.
			if err != nil && !errors.Is(err, rpc.ErrFrameTooLarge) {
				return
			}
		case <-s.stopped:
			return
		case <-s.conn.Done():
			return
		}
	}
}
