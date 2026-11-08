package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness"
)

// Before anything is typed into a harness the screen is checked for a
// dialog that would take the keys instead: an update offer (OpenCode 1.x
// confirmed its update when a typed prompt hit its dialog), a folder trust
// question, an answer menu. While one is up nothing is typed. The guard
// waits a moment for it to close, then leaves the text untyped, puts a
// notice in the chat and tells the phone the session needs the user. An
// answer menu the pane watcher recognises also reaches the phone as an
// approval through the watcher; the guard only keeps typing out of it.

// dialogWait is how long typing waits for a dialog to close.
var dialogWait = 3 * time.Second

// screenClear returns nil when no dialog is on the session's screen, or a
// FallbackError naming the one that stayed up for dialogWait. A screen
// that cannot be read does not stop typing: there is nothing to judge.
func (e *Engine) screenClear(ctx context.Context, s *sess) error {
	if e.cfg.Screen == nil {
		return nil
	}
	deadline := time.Now().Add(e.scaled(dialogWait))
	dismissed := false
	for {
		screen, err := e.cfg.Screen(ctx, s.id)
		if err != nil {
			return nil
		}
		d, ok := detect.Dialog(screen)
		if !ok {
			s.mu.Lock()
			s.dialog = ""
			s.mu.Unlock()
			return nil
		}
		if !dismissed && d.Prompt == nil && safeDismiss(d.Dismiss) && e.cfg.Typist != nil {
			// A splash with a key that only closes it (Cline's "Press
			// Enter to open, any other key to close"): close it, never
			// with Enter, once, and look again.
			dismissed = true
			if err := e.cfg.Typist.Keys(ctx, s.id, d.Dismiss...); err == nil {
				deadline = time.Now().Add(e.scaled(dialogWait))
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(e.scaled(verifyPoll)):
				}
				continue
			}
		}
		e.dialogSeen(s, d)
		if time.Now().After(deadline) {
			return &harness.FallbackError{Reason: "source", Detail: dialogText(s, d)}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.scaled(verifyPoll)):
		}
	}
}

// dialogSeen reports a dialog once until the screen is clear again.
func (e *Engine) dialogSeen(s *sess, d detect.DialogInfo) {
	s.mu.Lock()
	if s.dialog == d.Title {
		s.mu.Unlock()
		return
	}
	s.dialog = d.Title
	it := harness.Item{ID: "dialog-" + e.newID(), Kind: harness.KindNotice, At: e.cfg.Now(),
		Notice: &harness.NoticeBody{Level: "warning", Code: harness.NoticeFallback, Text: dialogText(s, d)}}
	s.publishLocked([]harness.Item{it}, nil)
	name := s.ad.Name
	s.mu.Unlock()
	if e.cfg.NeedsYou != nil {
		e.cfg.NeedsYou(s.id, name+" needs you", d.Title+": answer it in the terminal.")
	}
}

func dialogText(s *sess, d detect.DialogInfo) string {
	title := d.Title
	if title == "" {
		title = "a dialog"
	}
	return s.ad.Name + " is showing " + quoteTitle(title) + ". Nothing was typed: answer it in the terminal, then send again."
}

func quoteTitle(t string) string {
	if t == "a dialog" {
		return t
	}
	return "“" + t + "”"
}

// dialogResult turns the guard's refusal into an action result that says
// why nothing was typed; any other error gives an empty result.
func dialogResult(err error) (harness.ActionResult, error) {
	var fb *harness.FallbackError
	if errors.As(err, &fb) {
		return harness.ActionResult{Result: harness.ResultFailed, Message: fb.Detail}, nil
	}
	return harness.ActionResult{}, err
}

// safeDismiss reports keys that may close a dialog unasked: some, and none
// of them Enter, which on a link splash opens the link.
func safeDismiss(keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		switch strings.ToLower(k) {
		case "enter", "c-m", "kpenter", "return", "c-j", "space":
			return false
		}
	}
	return true
}
