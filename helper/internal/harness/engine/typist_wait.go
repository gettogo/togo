package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Typing into a live session is guarded three ways: one writer at a time
// per session (a typed command and a queued prompt never interleave), a
// wait for an idle prompt before anything is typed, and a refusal of
// anything that would drive a menu: only the allow-listed keys are sent,
// typed commands are one line, and text holds no control characters.

// idleSettle is how long the agent must stay idle before typing, so a
// turn boundary (tool result in, next request out) does not read as idle.
const idleSettle = 300 * time.Millisecond

// typing holds one lock per session id.
var typing sync.Map // string -> *sync.Mutex

func typingLock(id string) *sync.Mutex {
	l, _ := typing.LoadOrStore(id, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// idleNow reports whether the session is at its prompt: no turn running,
// by the mappers or the pane watcher, and no approval waiting (typed text
// would answer the approval's menu).
func (e *Engine) idleNow(s *sess) bool {
	s.mu.Lock()
	working := s.stateLocked()["working"] == "true"
	s.mu.Unlock()
	if working {
		return false
	}
	if e.cfg.Approvals != nil && len(e.cfg.Approvals.Pending(s.id)) > 0 {
		return false
	}
	return true
}

// waitIdle waits until the session has been idle for idleSettle, at most
// idleWait; past that it is ErrBusy.
func (e *Engine) waitIdle(ctx context.Context, s *sess) error {
	deadline := time.Now().Add(e.scaled(idleWait))
	var since time.Time
	for {
		if e.idleNow(s) {
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= e.scaled(idleSettle) {
				return nil
			}
		} else {
			since = time.Time{}
		}
		if time.Now().After(deadline) {
			return harness.ErrBusy
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.scaled(verifyPoll)):
		}
	}
}

// typeCommand types a documented command (or, for the composer, a prompt)
// into the session, one writer at a time, after an idle wait when asked.
// The idle wait holds no lock, so an interrupt or a queued prompt is not
// held up behind it; idleness is checked again under the lock.
func (e *Engine) typeCommand(ctx context.Context, s *sess, text string, submit, whenIdle bool) error {
	l := typingLock(s.id)
	for {
		if whenIdle {
			if err := e.waitIdle(ctx, s); err != nil {
				return err
			}
		}
		l.Lock()
		if !whenIdle || e.idleNow(s) {
			defer l.Unlock()
			if err := e.screenClear(ctx, s); err != nil {
				return err
			}
			return e.typeChecked(ctx, s, text, submit)
		}
		l.Unlock()
	}
}

// sendKeys sends allow-listed keys, one writer at a time. BTab is sent only
// by a verified cycle.
func (e *Engine) sendKeys(ctx context.Context, s *sess, cycling bool, keys ...string) error {
	for _, k := range keys {
		if !spec.AllowedKeys[k] || (k == "BTab" && !cycling) {
			return errKeyRefused
		}
	}
	if !interruptKeys(keys) {
		l := typingLock(s.id)
		l.Lock()
		defer l.Unlock()
		if err := e.screenClear(ctx, s); err != nil {
			return err
		}
	}
	return e.cfg.Typist.Keys(ctx, s.id, keys...)
}

// interruptKeys reports whether keys only interrupt (Escape, Ctrl-C):
// they are safe between any other writes and must never wait for one.
func interruptKeys(keys []string) bool {
	for _, k := range keys {
		if k != "Escape" && k != "C-c" {
			return false
		}
	}
	return len(keys) > 0
}

type keyRefused struct{}

func (keyRefused) Error() string { return "harness: that key is not sent" }

var errKeyRefused error = keyRefused{}

// commandText checks typed text: a command is one line starting with "/";
// only the composer's prompt may span lines.
func commandText(text string, prompt bool) (string, bool) {
	text = strings.TrimSpace(text)
	if prompt {
		return text, text != ""
	}
	return text, strings.HasPrefix(text, "/") && !strings.ContainsAny(text, "\r\n") && len(text) > 1
}
