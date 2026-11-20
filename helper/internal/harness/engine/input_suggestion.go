package engine

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A harness can draw a suggestion in its empty input line that no key
// removes. Seen live on omp 19.0.1: after each turn it predicts the next
// message and draws it after the cursor in its theme's muted colour (a
// 24-bit colour, not dim, so readInput cannot tell it from typed text).
// End and C-u leave it, backspaces leave it, Enter does not send it. Read
// as typed text, it made every prompt from the phone fail with "the input
// line already holds text".
//
// So, for an adapter that declares Suggestion: text that the clear keys
// and the backspaces left exactly as it was, standing after the cursor, is
// the suggestion. It is remembered for the session, and the line reads as
// empty while it shows exactly that text with the cursor before it. The
// clear keys always go out first when the line shows anything, so a draft
// that happens to read the same is removed, never taken for it.
//
// The remembered text is forgotten when the line shows other text before
// anything is typed, when a turn starts (omp predicts afresh after each
// turn), when the agent is relaunched and when the chat follows another
// conversation.

// clearSettle bounds the wait for the line to repaint after the clear keys.
const clearSettle = 600 * time.Millisecond

// keptSuggestion reports, after a clear, whether the text left in the line
// is the harness's suggestion: the backspaces left what the clear keys
// left, a later read still shows it, and the cursor stands before it. It
// remembers the text.
func (e *Engine) keptSuggestion(ctx context.Context, s *sess, kept, cur string) bool {
	if s.ad.Input == nil || !s.ad.Input.Suggestion || cur == "" || cur != kept {
		return false
	}
	// One more read: a screen not yet repainted after the keys would
	// otherwise be remembered.
	if !e.pause(ctx) {
		return false
	}
	if again, ok := e.inputRaw(ctx, s); !ok || again != cur || !e.cursorBefore(ctx, s, cur) {
		return false
	}
	s.mu.Lock()
	s.suggestion = cur
	s.mu.Unlock()
	return true
}

// shownSuggestion reports whether text is the suggestion a clear found,
// still shown after the cursor.
func (e *Engine) shownSuggestion(ctx context.Context, s *sess, text string) bool {
	if text == "" || s.ad.Input == nil || !s.ad.Input.Suggestion {
		return false
	}
	s.mu.Lock()
	known := s.suggestion
	s.mu.Unlock()
	return known != "" && text == known && e.cursorBefore(ctx, s, text)
}

// lineBeforeTyping forgets the remembered suggestion when the line, read
// before anything is typed, shows other text: the user typed, or the
// harness suggests something else. Text the helper itself typed is read
// later and does not count.
func (s *sess) lineBeforeTyping(text string) {
	s.mu.Lock()
	if s.suggestion != text {
		s.suggestion = ""
	}
	s.mu.Unlock()
}

// forgetSuggestionLocked drops the remembered suggestion. s.mu is held.
func (s *sess) forgetSuggestionLocked() { s.suggestion = "" }

// repainted waits, up to clearSettle, for the input line to read other
// than before (the keys took effect), and returns what it reads then.
func (e *Engine) repainted(ctx context.Context, s *sess, before string) (string, bool) {
	deadline := time.Now().Add(e.scaled(clearSettle))
	for {
		if !e.pause(ctx) {
			return "", false
		}
		cur, ok := e.inputRaw(ctx, s)
		if !ok || cur != before || time.Now().After(deadline) {
			return cur, ok
		}
	}
}

// typeOverSuggestion types a prompt for an adapter that draws suggestions
// and presses Enter only once the line shows the prompt. Typed text that
// does not match the suggestion replaces it; typed text that begins it
// leaves the rest drawn after the cursor, and Enter still sends only what
// was typed (checked live on omp 19.0.1: "now" typed over "now let me show
// you ..." was sent as "now"). A line that never shows the prompt is
// reported, and nothing is sent.
func (e *Engine) typeOverSuggestion(ctx context.Context, s *sess, text string) error {
	if err := e.cfg.Typist.Type(ctx, s.id, text, false, false); err != nil {
		return err
	}
	deadline := time.Now().Add(e.scaled(submitWait))
	for {
		cur, ok := e.inputRaw(ctx, s)
		if ok && holds(cur, text) {
			break
		}
		if time.Now().After(deadline) {
			_ = e.clearInput(ctx, s)
			return &harness.FallbackError{Reason: "source",
				Detail: s.ad.Name + "'s input line did not show the prompt as typed. Nothing was sent."}
		}
		if !e.pause(ctx) {
			return ctx.Err()
		}
	}
	return e.cfg.Typist.Keys(ctx, s.id, "Enter")
}

// cursorBefore reports whether the cursor is shown in the line that holds
// text, at or before the column where text starts. A cursor that cannot be
// read proves nothing.
func (e *Engine) cursorBefore(ctx context.Context, s *sess, text string) bool {
	x, line, visible, ok := e.cursorAt(ctx, s)
	if !ok || !visible {
		return false
	}
	line = plainSpaces(line)
	i := strings.Index(line, text)
	if i < 0 {
		return false
	}
	// Columns count runes: a wide character before the text puts the
	// cursor further right than its rune count, which reads as "after".
	return x <= utf8.RuneCountInString(line[:i])
}
