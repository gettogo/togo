package push

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

const ellipsis = "…"

// shortSubject cuts a subject's text to at most n runes in the way that
// keeps its meaning: a command loses its middle, so the program and the
// last arguments stay; a path keeps its file name and the folders nearest
// it; a URL loses its middle; free text loses its end. The result still
// passes Subject.Validate when s did.
func shortSubject(s wire.Subject, n int) wire.Subject {
	if n < 2 {
		n = 2
	}
	switch s.Kind {
	case wire.SubjectCommand:
		s.Text = middle(strings.ReplaceAll(detect.CleanLine(s.Text), "\n", " "), n)
	case wire.SubjectFile:
		s.Text = shortPaths(s.Text, n)
	case wire.SubjectURL:
		s.Text = middle(strings.TrimSpace(s.Text), n)
	default:
		s.Text = end(detect.CleanLine(s.Text), n)
	}
	if s.Text == "" {
		s.Text = ellipsis
	}
	return s
}

// middle cuts s to at most n runes by taking out its middle: three fifths
// of the room for the start, the rest for the end.
func middle(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	if n < 3 {
		return string(r[:n])
	}
	head := (n - 1) * 3 / 5
	tail := n - 1 - head
	return strings.TrimRight(string(r[:head]), " ") + ellipsis + strings.TrimLeft(string(r[len(r)-tail:]), " ")
}

// end cuts s to at most n runes, ending in an ellipsis when it was cut.
func end(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n < 2 {
		return truncate(s, n)
	}
	return strings.TrimRight(truncate(s, n-1), " ") + ellipsis
}

// homeDir matches a home folder at the start of an absolute path.
var homeDir = regexp.MustCompile(`^(/Users|/home)/[^/]+(/|$)`)

// tempDir matches the system's temporary folders, whose inner names are
// generated and say nothing to a person.
var tempDir = regexp.MustCompile(`^(/private)?(/tmp|/var/folders|/var/tmp)/`)

// maxPushPath is how long a file subject in an alert may be before it is
// shortened like a path.
const maxPushPath = 120

// shortPaths shortens each line of a file subject (one path per line) to
// share n runes, at most three lines.
func shortPaths(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	each := max(8, (n-len(lines)+1)/len(lines))
	for i, l := range lines {
		lines[i] = shortPath(strings.TrimSpace(l), each)
	}
	return truncate(strings.Join(lines, "\n"), n)
}

// shortPath cuts a path to at most n runes: the home folder reads as ~,
// then folders go from the middle, keeping the first and as many of the
// last as fit, so the file name always shows.
func shortPath(p string, n int) string {
	if tempDir.MatchString(p) {
		// Only the last two folders mean anything: "…/harness-e2e/kimi".
		parts := strings.Split(strings.TrimRight(p, "/"), "/")
		if len(parts) > 3 {
			p = ellipsis + "/" + strings.Join(parts[len(parts)-2:], "/")
		}
		if utf8.RuneCountInString(p) <= n {
			return p
		}
	}
	p = homeDir.ReplaceAllString(p, "~/")
	if utf8.RuneCountInString(p) <= n {
		return p
	}
	parts := strings.Split(p, "/")
	if len(parts) < 3 {
		return middle(p, n)
	}
	name := parts[len(parts)-1]
	if utf8.RuneCountInString(name)+2 > n {
		return middle(name, n)
	}
	first := parts[0] // "" for an absolute path, "~" or a folder otherwise
	// Folders nearest the file, while "<first>/…/<kept>/<name>" fits.
	kept := []string{name}
	for i := len(parts) - 2; i >= 1; i-- {
		next := append([]string{parts[i]}, kept...)
		if utf8.RuneCountInString(first+"/"+ellipsis+"/"+strings.Join(next, "/")) > n {
			break
		}
		kept = next
	}
	out := first + "/" + ellipsis + "/" + strings.Join(kept, "/")
	if utf8.RuneCountInString(out) > n {
		out = ellipsis + "/" + strings.Join(kept, "/")
	}
	if utf8.RuneCountInString(out) > n {
		return middle(name, n)
	}
	return out
}
