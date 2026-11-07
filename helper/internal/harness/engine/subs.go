package engine

import (
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// QueueSize bounds the notifications waiting for one subscriber of one
// session. A subscriber that falls this far behind gets a reset instead.
const QueueSize = 256

// subQueue delivers one session's notifications to one subscriber, in
// order, on its own goroutine. It starts held: notifications queue until
// release, so the open response is written first.
type subQueue struct {
	sub     harness.Subscriber
	session string
	epoch   func() string
	now     func() time.Time
	onDrop  func()

	mu       sync.Mutex
	q        []harness.Notification
	held     bool
	dead     bool
	overflow bool
	wake     chan struct{}
}

func newSubQueue(sub harness.Subscriber, session string, epoch func() string, now func() time.Time, onDrop func()) *subQueue {
	s := &subQueue{sub: sub, session: session, epoch: epoch, now: now, onDrop: onDrop, held: true, wake: make(chan struct{}, 1)}
	go s.run()
	return s
}

// push queues n. On overflow the queue is dropped and replaced by one
// reset, after which the subscriber gets nothing more until it reopens.
func (s *subQueue) push(n harness.Notification) {
	s.mu.Lock()
	if s.dead || s.overflow {
		s.mu.Unlock()
		return
	}
	if len(s.q) >= QueueSize {
		s.overflow = true
		s.q = []harness.Notification{{
			SessionID: s.session, Kind: harness.NoteReset, TS: s.now(), Epoch: s.epoch(), Reason: "overflow",
		}}
	} else {
		s.q = append(s.q, n)
	}
	s.mu.Unlock()
	s.signal()
}

func (s *subQueue) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// release lets queued notifications flow.
func (s *subQueue) release() {
	s.mu.Lock()
	s.held = false
	s.mu.Unlock()
	s.signal()
}

func (s *subQueue) close() {
	s.mu.Lock()
	s.dead = true
	s.q = nil
	s.mu.Unlock()
	s.signal()
}

func (s *subQueue) run() {
	for {
		select {
		case <-s.sub.Done():
			s.close()
			if s.onDrop != nil {
				s.onDrop()
			}
			return
		case <-s.wake:
		}
		for {
			s.mu.Lock()
			if s.dead {
				s.mu.Unlock()
				return
			}
			if s.held || len(s.q) == 0 {
				s.mu.Unlock()
				break
			}
			n := s.q[0]
			s.q = s.q[1:]
			last := s.overflow && len(s.q) == 0
			s.mu.Unlock()
			if err := s.sub.Notify(n); err != nil {
				// The notification could not be sent (too large for a
				// frame, or the connection failed): tell the phone to
				// reopen rather than leave it silently behind.
				_ = s.sub.Notify(harness.Notification{
					SessionID: s.session, Kind: harness.NoteReset, TS: s.now(), Epoch: s.epoch(), Reason: "overflow",
				})
				s.close()
				if s.onDrop != nil {
					s.onDrop()
				}
				return
			}
			if last {
				// The reset went out; the subscriber must reopen.
				s.close()
				if s.onDrop != nil {
					s.onDrop()
				}
				return
			}
		}
	}
}
