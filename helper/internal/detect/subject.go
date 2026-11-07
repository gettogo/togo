package detect

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/gettogo/togo/helper/internal/wire"
)

// maxAbove is how far above an unboxed question the subject may be. Inside
// a box the whole dialog is searched; outside one, lines further up are
// agent output rather than part of the prompt.
const maxAbove = 10

// displayANSI matches CSI and OSC sequences, including one cut off before
// its final byte, the same way the app's PromptText does.
var displayANSI = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]?|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)?")

// CleanLine keeps what a person would read from text captured off a
// terminal: ANSI sequences, box drawing (U+2500-U+257F), block elements
// (U+2580-U+259F) and invisible characters removed, other control
// characters turned into spaces, a menu cursor (❯ › ▸ >) at the start of a
// line removed, runs of white space collapsed, blank lines dropped and at
// most three lines kept. It is the twin of the app's PromptText.clean;
// protocol/fixtures/render/clean.json is the table both are tested on.
func CleanLine(s string) string {
	var lines []string
	s = displayANSI.ReplaceAllString(strings.ToValidUTF8(s, ""), "")
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.Map(func(r rune) rune {
			switch {
			case r >= 0x2500 && r <= 0x259F, wire.IsInvisible(r):
				return -1
			case unicode.IsControl(r):
				return ' '
			}
			return r
		}, line)
		line = strings.TrimSpace(line)
		for _, cursor := range []string{"❯", "›", "▸", ">"} {
			if rest, ok := strings.CutPrefix(line, cursor); ok {
				line = rest
				break
			}
		}
		if line = strings.Join(strings.Fields(line), " "); line == "" {
			continue
		}
		if lines = append(lines, line); len(lines) == 3 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// NewSubject cleans text with CleanLine and caps it to the schema's length,
// so the result always passes wire.Subject.Validate. It returns nil when
// nothing readable is left or kind is unknown.
func NewSubject(kind, text string) *wire.Subject {
	if kind != wire.SubjectCommand {
		// A key-hint footer read off the screen is never what is asked about.
		text = DropKeyHints(text)
	}
	s := wire.Subject{Kind: kind, Text: truncate(CleanLine(text), wire.MaxSubjectText)}
	if s.Text == "" || s.Validate() != nil {
		return nil
	}
	return &s
}

// SubjectKindForTool names what an agent tool call is about: shell tools
// run a command, editing tools touch a file, fetch tools open a URL, and
// anything else is shown as text.
func SubjectKindForTool(tool string) string {
	switch strings.ToLower(tool) {
	case "bash", "shell", "exec", "exec_command", "local_shell", "run", "run_command", "unified_exec",
		"run_shell_command", "shell_command", "execute", "powershell":
		return wire.SubjectCommand
	case "edit", "multiedit", "multi_edit", "write", "notebookedit", "apply_patch", "applypatch", "patch",
		"edit_file", "write_file", "create_file", "delete_file", "create", "replace", "search_replace",
		"str_replace", "str_replace_editor":
		return wire.SubjectFile
	case "webfetch", "web_fetch", "fetch":
		return wire.SubjectURL
	}
	return wire.SubjectText
}

// labelKinds are dialog headings whose next line is the subject.
var labelKinds = map[string]string{
	"bash command":  wire.SubjectCommand,
	"shell command": wire.SubjectCommand,
	"command":       wire.SubjectCommand,
	"edit file":     wire.SubjectFile,
	"write file":    wire.SubjectFile,
	"create file":   wire.SubjectFile,
	"update file":   wire.SubjectFile,
	"delete file":   wire.SubjectFile,
	"fetch":         wire.SubjectURL,
	"web fetch":     wire.SubjectURL,
	"fetch url":     wire.SubjectURL,
}

// inline are one-line shapes that carry the subject themselves.
var inline = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`^\$ (\S.*)$`), wire.SubjectCommand},
	{regexp.MustCompile(`(?i)^(?:bash|shell|exec|command|run): (\S.*)$`), wire.SubjectCommand},
	{regexp.MustCompile(`(?i)^(?:edit|write|create|patch|delete): (\S.*)$`), wire.SubjectFile},
	{regexp.MustCompile(`(?i)^(?:fetch|webfetch|url): (\S.*)$`), wire.SubjectURL},
	{regexp.MustCompile(`^(?:write|edit|create|delete)_file (\S+)`), wire.SubjectFile},
	{regexp.MustCompile(`^• (?:Proposed change to|Edit|Edited|Add|Added|Delete|Deleted|Update|Updated) (.+?)(?: \(\+\d+ -\d+\))?$`), wire.SubjectFile},
	{regexp.MustCompile(`^https?://\S+$`), wire.SubjectURL},
}

// subjectOf finds the command, file or URL a menu prompt asks about: first
// in the lines between the question (header) and the options (start), then
// upwards from the question, nearest first. The search above stays inside
// the question's box, or within maxAbove lines when it has none.
func subjectOf(lines []string, boxed []bool, header, start int) *wire.Subject {
	below := cleaned(lines[header+1 : start])
	if s := scan(below, false); s != nil {
		return s
	}
	lo := header
	for i := header - 1; i >= 0; i-- {
		if boxed[header] && !boxed[i] || !boxed[header] && header-i > maxAbove {
			break
		}
		lo = i
	}
	if s := fenced(lines[lo:header]); s != nil {
		return s
	}
	return scan(cleaned(lines[lo:header]), true)
}

// fenced reads Claude Code 2.1.3xx's dialog: a heading ("Bash command"),
// a tip and a description, then the command between two dashed rules. The
// line after the heading is not the subject there.
func fenced(lines []string) *wire.Subject {
	kind := ""
	for i, l := range lines {
		if k, ok := labelKinds[strings.ToLower(strings.TrimSuffix(CleanLine(l), ":"))]; ok {
			kind = k
			continue
		}
		if kind == "" || !isDashed(l) {
			continue
		}
		for _, n := range lines[i+1:] {
			if isDashed(n) {
				break
			}
			if c := CleanLine(n); c != "" {
				return NewSubject(kind, c)
			}
		}
		return nil
	}
	return nil
}

// isDashed reports whether l is a dashed rule (╌╌╌), which Claude draws
// around the command in a permission dialog.
func isDashed(l string) bool {
	t := strings.TrimSpace(l)
	if t == "" {
		return false
	}
	for _, r := range t {
		if !strings.ContainsRune("╌╍┄┅┈┉", r) {
			return false
		}
	}
	return true
}

// cleaned runs every line through CleanLine on its own, so nested box
// edges and alignment go away while the line positions stay put.
func cleaned(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = CleanLine(l)
	}
	return out
}

// scan looks for a subject in lines, top down or bottom up.
func scan(lines []string, bottomUp bool) *wire.Subject {
	for k := range lines {
		i := k
		if bottomUp {
			i = len(lines) - 1 - k
		}
		l := lines[i]
		if kind, ok := labelKinds[strings.ToLower(strings.TrimSuffix(l, ":"))]; ok {
			for _, next := range lines[i+1:] {
				if next != "" {
					return NewSubject(kind, next)
				}
			}
			continue
		}
		for _, p := range inline {
			if g := p.re.FindStringSubmatch(l); g != nil {
				text := g[0]
				if len(g) > 1 {
					text = g[1]
				}
				return NewSubject(p.kind, text)
			}
		}
	}
	return nil
}
