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

// heldBecause puts on record why a queued prompt is not being typed, once
// per reason: the drain tries again every drainRetry for as long as the
// prompt waits, and a prompt held for minutes left no trace of the cause.
func (e *Engine) heldBecause(s *sess, why string) {
	if was, ok := e.heldWhy.Load(s.id); ok && was == why {
		return
	}
	e.heldWhy.Store(s.id, why)
	e.cfg.Logf("harness: %s holds a queued prompt: %s", s.id, why)
}

// typeNext types the first queued prompt under the typing lock, after
// checking again that the agent is idle. It reports whether a prompt was
// typed.
func (e *Engine) typeNext(s *sess) bool {
	l := typingLock(s.id)
	l.Lock()
	defer l.Unlock()
	api, viaAPI := e.sendAction(s)
	if !viaAPI && e.cfg.Typist == nil {
		return false
	}
	if !e.idleNow(s) {
		e.heldBecause(s, "the session is working or an approval waits")
		return false
	}
	if why := e.asking(s.ctx, s, viaAPI); why != "" {
		// A question on screen holds the queue until it is answered.
		e.heldBecause(s, why)
		return false
	}
	e.heldWhy.Delete(s.id)
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.markSentLocked(next.item.ID)
	gen := s.convGen
	u := next.item.User
	// Noted before it is typed: the agent's record of it may be read
	// before typing returns (see phoneText).
	s.pendPhoneLocked(next.item.ID, u.Text, u.Images)
	s.mu.Unlock()
	var err error
	if viaAPI {
		err = e.sendAPI(s.ctx, s, api, u.Text, harness.ImagePaths(u.Images))
	} else {
		err = e.typePrompt(s.ctx, s, u.Text, harness.ImagePaths(u.Images))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.convGen != gen {
		// The chat started over while the prompt travelled. It is not
		// put back, shown as sent, or sent again; the new chat says so.
		s.settlePhoneLocked(next.item.ID, false)
		s.uncertainNoticeLocked(u.Text)
		return false
	}
	if s.settlePhoneLocked(next.item.ID, err == nil) {
		// The agent recorded it while typing was still waiting: its
		// record took the queued item's place, and it was sent.
		if err != nil {
			e.cfg.Logf("harness: sending a queued prompt reported %v, but the agent recorded it", err)
		}
		return true
	}
	if err != nil {
		e.cfg.Logf("harness: sending a queued prompt failed: %v", err)
		if viaAPI {
			// A request that failed may still have reached the harness, so
			// it is not sent again: the prompt leaves the queue and the
			// chat says so.
			s.dropUnsentLocked(next.item)
			return false
		}
		delete(s.sent, next.item.ID)
		s.queue = append([]queued{next}, s.queue...)
		return false
	}
	it := next.item
	body := *u
	body.Queued = false
	it.User = &body
	s.awaitLocked(u.Text, s.phoneEndsLocked(next.item.ID))
	s.publishLocked([]harness.Item{it}, nil)
	return true
}

// dropUnsentLocked withdraws a queued prompt whose API request failed and leaves
// a notice with its text, so the user can send it again.
func (s *sess) dropUnsentLocked(it harness.Item) {
	e := s.e
	u := *it.User
	u.Queued, u.Withdrawn = false, true
	it.User = &u
	text := s.ad.Name + " returned an error for a queued prompt. It may not have arrived and was not sent again: " + clipNotice(u.Text)
	s.publishLocked([]harness.Item{it, {ID: "unsent-" + it.ID, Kind: harness.KindNotice, At: e.cfg.Now(),
		Notice: &harness.NoticeBody{Level: "error", Code: harness.NoticeError, Reason: "refused", Text: text}}}, nil)
}

// clipNotice shortens a prompt quoted inside a notice.
func clipNotice(text string) string {
	const limit = 200
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit]) + "…"
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
