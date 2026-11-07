package engine

import (
	"context"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// drainWait bounds one wait for the prompt to come back; while prompts
// stay queued the drain keeps trying, drainRetry apart.
const drainWait = 30 * time.Second

var (
	drainRetry = 2 * time.Second
	// drainAfter is how long after typing a queued prompt the next one is
	// looked at, should no turn end arrive to start it (the stop hook came
	// before the status flipped, or only the pane watcher saw the turn).
	drainAfter = 5 * time.Second
)

// draining marks sessions with a drain in progress.
var draining sync.Map // session id -> struct{}

// drainQueue types the oldest queued prompt once the agent is idle. One
// drain runs per session at a time and types one prompt: that prompt
// starts a turn, and the turn's end (or a later check) drains the next, so
// prompts go in the order they were queued. The wait for idle holds no
// lock, so an interrupt is never held up; the prompt is taken off the
// queue before it is typed, so a cancel either removes it first or is told
// it was already sent. A prompt that cannot be typed goes back to the
// front.
func (e *Engine) drainQueue(s *sess) {
	if _, busy := draining.LoadOrStore(s.id, struct{}{}); busy {
		return
	}
	defer draining.Delete(s.id)
	for {
		if s.ctx.Err() != nil || s.queueLen() == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, e.scaled(drainWait))
		err := e.waitIdle(ctx, s)
		cancel()
		if err == nil && e.typeNext(s) {
			if s.queueLen() > 0 {
				time.AfterFunc(e.scaled(drainAfter), func() { e.drainQueue(s) })
			}
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(e.scaled(drainRetry)):
		}
	}
}

// typeNext types the first queued prompt under the typing lock, after
// checking again that the agent is idle. It reports whether a prompt was
// typed.
func (e *Engine) typeNext(s *sess) bool {
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	if !e.idleNow(s) {
		return false
	}
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.markSentLocked(next.item.ID)
	s.mu.Unlock()
	u := next.item.User
	if err := e.typePrompt(s.ctx, s, u.Text, harness.ImagePaths(u.Images)); err != nil {
		e.cfg.Logf("harness: typing a queued prompt failed: %v", err)
		s.mu.Lock()
		delete(s.sent, next.item.ID)
		s.queue = append([]queued{next}, s.queue...)
		s.mu.Unlock()
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := next.item
	body := *u
	body.Queued = false
	it.User = &body
	s.notePhoneLocked(it.ID, u.Text, u.Images)
	s.publishLocked([]harness.Item{it}, nil)
	return true
}

func (s *sess) queueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// maxSent bounds the ids of queued prompts remembered as typed.
const maxSent = 32

func (s *sess) markSentLocked(id string) {
	if s.sent == nil || len(s.sent) >= maxSent {
		s.sent = map[string]bool{}
	}
	s.sent[id] = true
}

// dropQueued removes a queued prompt the user cancelled, by its item id.
// queued is false when id names no queued prompt; sent is true when it
// named one that was already typed.
func (e *Engine) dropQueued(s *sess, id string) (queued, sent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.queue {
		if q.item.ID != id {
			continue
		}
		s.queue = append(s.queue[:i:i], s.queue[i+1:]...)
		// Resent withdrawn, so open chats drop it and a reopen never shows it.
		it := q.item
		if it.User != nil {
			u := *it.User
			u.Queued, u.Withdrawn = false, true
			it.User = &u
			s.publishLocked([]harness.Item{it}, nil)
		}
		return true, false
	}
	return false, s.sent[id]
}
