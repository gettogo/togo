package engine

import (
	"context"
	"strings"
	"unicode/utf8"
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
// empty while it shows exactly that text with the cursor before it. A
// draft the user typed is removed by the clear keys, so it never qualifies.

// keptSuggestion reports, after a clear, whether the text left in the line
// is the harness's suggestion: the backspaces left what the clear keys
// left, and the cursor stands before it. It remembers the text.
func (e *Engine) keptSuggestion(ctx context.Context, s *sess, kept, cur string) bool {
	if s.ad.Input == nil || !s.ad.Input.Suggestion || cur == "" || cur != kept {
		return false
	}
	if !e.cursorBefore(ctx, s, cur) {
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
