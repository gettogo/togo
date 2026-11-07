package hooks

import (
	"errors"
	"path/filepath"
	"strings"
)

// shellQuote single-quotes s for the sh -c line agents run hook commands
// through, so a binary path with spaces or quotes stays one word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hookCommand(binary, agent, event string) string {
	return shellQuote(binary) + " hook " + agent + " " + event
}

// ErrEdited is why install refuses a harness: one of Togo's entries was
// changed by hand, and install does not overwrite it.
var ErrEdited = errors.New("a Togo hook entry was changed by hand; edit it back or run togo hooks uninstall first")

// plainCommand reports whether cmd is exactly the form hookCommand writes
// (one word naming a togo binary, then "hook <agent> <event>"), so an
// install may update it, for a new binary path, without losing an edit.
func plainCommand(cmd, agent string) bool {
	f := strings.Fields(cmd)
	if len(f) < 4 || f[len(f)-3] != "hook" || f[len(f)-2] != agent {
		return false
	}
	tail := strings.Join(f[len(f)-3:], " ")
	if !strings.HasSuffix(cmd, " "+tail) {
		return false
	}
	word, ok := shellWord(strings.TrimSuffix(cmd, " "+tail))
	return ok && strings.Contains(filepath.Base(word), "togo")
}

// shellWord undoes shellQuote, or takes a bare word with no shell syntax.
func shellWord(s string) (string, bool) {
	if !strings.HasPrefix(s, "'") {
		return s, s != "" && !strings.ContainsAny(s, " \t;&|$`<>\"'\\(){}*?")
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2
		case strings.HasPrefix(s[i:], `\'`):
			b.WriteByte('\'')
			i += 2
		default:
			return "", false
		}
	}
	return b.String(), true
}

// owned reports whether a hook command is one togo installed for agent.
func owned(cmd, agent string) bool {
	f := strings.Fields(cmd)
	return len(f) >= 3 && f[len(f)-3] == "hook" && f[len(f)-2] == agent
}
