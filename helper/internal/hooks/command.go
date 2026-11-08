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

// hookCommand is the hook line Togo installs. For harnesses that run hook
// commands through a shell it is guarded: when the togo binary is gone
// (rm -rf ~/.togo without togo uninstall) the hook exits 0 silently
// instead of failing every event with "command not found".
func hookCommand(binary, agent, event string) string {
	bare := bareHookCommand(binary, agent, event)
	if !shellHooks[agent] {
		return bare
	}
	return guardOpen(binary) + bare + guardTrue
}

func bareHookCommand(binary, agent, event string) string {
	return shellQuote(binary) + " hook " + agent + " " + event
}

// shellHooks are the harnesses known to run a hook's command through sh,
// so the guard's shell syntax works there.
var shellHooks = map[string]bool{Claude: true, Copilot: true}

const (
	guardTrue     = " || true"
	guardThenExec = " || exec sh -c "
)

func guardOpen(binary string) string { return "[ -x " + shellQuote(binary) + " ] && exec " }

// unguard returns the command inside a guard hookCommand or chainCommand
// wrote, and whether cmd was guarded at all. A guard whose test names a
// different binary than the one it runs is not Togo's form.
func unguard(cmd string) (string, bool) {
	if !strings.HasPrefix(cmd, "[ -x ") {
		return cmd, false
	}
	i := strings.Index(cmd, " ] && exec ")
	if i < 0 {
		return cmd, false
	}
	bin, inner := cmd[len("[ -x "):i], cmd[i+len(" ] && exec "):]
	switch {
	case strings.HasSuffix(inner, guardTrue):
		inner = strings.TrimSuffix(inner, guardTrue)
	case strings.Contains(inner, guardThenExec):
		inner = inner[:strings.LastIndex(inner, guardThenExec)]
	default:
		return cmd, false
	}
	if !strings.HasPrefix(inner, bin+" ") {
		return cmd, false
	}
	return inner, true
}

// ErrEdited is why install refuses a harness: one of Togo's entries was
// changed by hand, and install does not overwrite it.
var ErrEdited = errors.New("a Togo hook entry was changed by hand; edit it back or run togo hooks uninstall first")

// plainCommand reports whether cmd is exactly the form hookCommand writes
// (one word naming a togo binary, then "hook <agent> <event>"), so an
// install may update it, for a new binary path, without losing an edit.
func plainCommand(cmd, agent string) bool {
	if inner, ok := unguard(cmd); ok {
		if !strings.HasSuffix(cmd, guardTrue) {
			return false
		}
		cmd = inner
	}
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
	cmd, _ = unguard(cmd)
	f := strings.Fields(cmd)
	return len(f) >= 3 && f[len(f)-3] == "hook" && f[len(f)-2] == agent
}
