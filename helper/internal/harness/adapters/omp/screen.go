package omp

import (
	"regexp"
	"strings"
)

// omp writes its session file only once the first turn starts, so a model
// switched before it (the usual case: sign in, pick a model, then ask)
// leaves no record to read for a while. The pane shows it at once: a
// successful /switch prints
//
//	Session-only model: anthropic/claude-opus-5-5. Use ⌥M or /model for roles.
//
// (seen on 18.16.0), wrapped at the pane's width. ReadScreen reads the
// last such line, so the helper can confirm the switch and show the model
// omp now uses before any record of it exists.

const switchedMarker = "Session-only model:"

var (
	ansi     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	switched = regexp.MustCompile(`^Session-only model:\s*(\S+?)\.\s*Use\b`)
)

// ReadScreen reads the model a /switch on screen made current: "model".
func (s *session) ReadScreen(text string) map[string]string {
	if m := SwitchedModel(text); m != "" {
		return map[string]string{"model": m}
	}
	return nil
}

// SwitchedModel returns the selector of the last "Session-only model" line
// on screen, joined back across the pane's wraps, or "".
func SwitchedModel(screen string) string {
	lines := strings.Split(ansi.ReplaceAllString(screen, ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		at := strings.Index(lines[i], switchedMarker)
		if at < 0 {
			continue
		}
		// The selector has no spaces, so the pieces of a wrapped line are
		// joined without one; the marker's own space is kept apart.
		var b strings.Builder
		b.WriteString(strings.TrimSpace(lines[i][at:]))
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			piece := strings.TrimSpace(lines[j])
			if piece == "" {
				break
			}
			b.WriteString(piece)
		}
		if m := switched.FindStringSubmatch(b.String()); m != nil {
			return m[1]
		}
		return ""
	}
	return ""
}
