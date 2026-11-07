package detect

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// hintPart is one "keys verb" part of a TUI's key-hint footer, such as
// "↑↓ navigate", "Enter select", "Esc to cancel" or "ctrl+c exit".
var hintPart = regexp.MustCompile(`(?i)^(?:[↑↓←→⏎↵⇥]+|enter|esc|escape|tab|shift\+tab|space|return|ctrl\+\S+|pgup|pgdn|[a-z])` +
	`(?:\s*/\s*\S+)?\s+(?:to\s+)?[a-z]+(?:\s[a-z]+)?$`)

// hintSplit separates the parts of a key-hint line.
var hintSplit = regexp.MustCompile(`\s*[·|•]\s*|\s{2,}|,\s+`)

// IsKeyHint reports whether line is a TUI's key-hint footer ("↑↓ navigate
// · Enter select · Esc exit"): every part is keys then a verb, and there
// are two parts or more, or one that starts with an arrow or Enter or Esc.
// Such a line says how to drive the terminal, nothing about the request.
func IsKeyHint(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	parts := hintSplit.Split(line, -1)
	n := 0
	for _, p := range parts {
		if p == "" {
			continue
		}
		if !hintPart.MatchString(p) {
			return false
		}
		n++
	}
	if n >= 2 {
		return true
	}
	low := strings.ToLower(line)
	return n == 1 && (strings.ContainsAny(low, "↑↓←→⏎↵") || strings.HasPrefix(low, "enter ") || strings.HasPrefix(low, "esc"))
}

// DropKeyHints removes key-hint lines from s.
func DropKeyHints(s string) string {
	if !strings.ContainsAny(s, "↑↓←→⏎↵·|") && !strings.Contains(strings.ToLower(s), "enter") && !strings.Contains(strings.ToLower(s), "esc") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if !IsKeyHint(CleanLine(l)) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// pathLine matches a line that is only a path: absolute or under home.
var pathLine = regexp.MustCompile(`^(?:~|/)[^\s:]*/[^\s:]*$`)

// pathSubject is a file subject from the first detail line that is only
// a path, as in a "Trust this folder?" dialog that names the folder.
func pathSubject(detail *string) *wire.Subject {
	if detail == nil {
		return nil
	}
	for _, l := range strings.Split(*detail, "\n") {
		if l = CleanLine(l); pathLine.MatchString(l) {
			return NewSubject(wire.SubjectFile, l)
		}
	}
	return nil
}
