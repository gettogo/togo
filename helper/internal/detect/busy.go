package detect

import "strings"

// busyLines is how many non-empty lines at the bottom of the screen may
// hold an agent's working indicator: it sits just above the input box,
// with at most a status line or two below it.
const busyLines = 12

// busyMarkers are the working indicators of agents that draw one, matched
// in lower case. Each shows it only while a turn runs, never at the idle
// input box:
//   - Claude Code: "✻ Thinking… (12s · esc to interrupt)"
//   - Codex: "• Working (5s • Esc to interrupt)"
//   - Gemini CLI: "⠏ Thinking… (esc to cancel, 12s)"; its dialogs say
//     "Esc to cancel" too, but never followed by a comma
//   - Qwen Code: "⠏ Thinking… (12s · esc to cancel)"; its dialogs end the
//     hint without the parenthesis
//   - Auggie: "Working (5s • esc to interrupt)"
//   - Pi: "Working... (escape to interrupt)"; its key hints say "to
//     interrupt" without the parenthesis
//
// Other agents fall back to sustained output (see the watch package).
var busyMarkers = map[string][]string{
	"claude": {"esc to interrupt"},
	"codex":  {"esc to interrupt"},
	"gemini": {"(esc to cancel,"},
	"qwen":   {"esc to cancel)"},
	"auggie": {"esc to interrupt"},
	"pi":     {"to interrupt)"},
}

// KnowsBusy reports whether agent draws a working indicator Busy can read.
func KnowsBusy(agent string) bool {
	_, ok := busyMarkers[agent]
	return ok
}

// Busy reports whether screen shows agent's working indicator near the
// bottom. It is false for an agent KnowsBusy does not know.
func Busy(agent, screen string) bool {
	markers := busyMarkers[agent]
	if len(markers) == 0 {
		return false
	}
	lines := window(screen)
	if len(lines) > busyLines {
		lines = lines[len(lines)-busyLines:]
	}
	for _, l := range lines {
		l = strings.ToLower(l)
		for _, m := range markers {
			if strings.Contains(l, m) {
				return true
			}
		}
	}
	return false
}
