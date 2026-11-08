package detect

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

var (
	// loginOption is a lone highlighted menu entry that starts a sign-in
	// (Auggie's first screen: "→ Login to continue").
	loginOption = regexp.MustCompile(`(?i)^\s*[` + cursorRunes + `]\s+((log ?in|sign ?in)\b.*)$`)
	// pressReturn is the hint under it.
	pressReturn = regexp.MustCompile(`(?i)\bpress (return|enter)\b`)
)

// matchLoginStart recognises a sign-in that waits for one key before it
// shows a page or code: a single highlighted "Login …" entry with a
// "press return" hint under it, at the bottom of the screen. Enter starts
// the flow, whose page is then a sign-in step of its own; until then the
// step names no page or code.
func matchLoginStart(lines []string) (Prompt, bool) {
	n := len(lines)
	for i := n - 1; i >= 0 && i >= n-3; i-- {
		m := loginOption.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		hinted := false
		for j := i + 1; j < n; j++ {
			if pressReturn.MatchString(lines[j]) {
				hinted = true
			}
		}
		if !hinted {
			return Prompt{}, false
		}
		return signInPrompt(strings.TrimSpace(m[1]), wire.SignIn{}), true
	}
	return Prompt{}, false
}
