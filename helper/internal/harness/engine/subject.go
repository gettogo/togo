package engine

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// sendableSubject returns sub with text made to fit what the app accepts
// of a subject: valid UTF-8 of at most wire.MaxSubjectText characters, no
// control characters but newline, nothing invisible. The app drops a whole
// tool item whose subject breaks those rules, and then reopens the chat on
// every update of it, so a command with a tab in it (a heredoc, a
// Makefile) must not go out as it is. Nil when nothing readable is left.
func sendableSubject(sub wire.Subject, text string) *wire.Subject {
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case wire.IsInvisible(r):
			return -1
		case unicode.IsControl(r):
			return ' '
		}
		return r
	}, strings.ToValidUTF8(text, ""))
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > wire.MaxSubjectText {
		text = strings.TrimSpace(string([]rune(text)[:wire.MaxSubjectText]))
	}
	sub.Text = text
	if sub.Validate() != nil {
		return nil
	}
	return &sub
}
