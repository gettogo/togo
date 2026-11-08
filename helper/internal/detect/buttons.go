package detect

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// wordButtons is a row of two word buttons that a single key answers, by
// the agents that draw one:
//   - Crush 0.100, on a folder it has not initialised: "Would you like to
//     initialize this project?" over "Yep!     Nope". Y starts its own
//     AGENTS.md turn, N declines for good (it writes .crush/init). Typing a
//     chat prompt into it pressed whichever of y or n the text held first,
//     so the prompt was lost (seen live).
var wordButtons = []struct{ yes, no string }{
	{"Yep!", "Nope"},
}

// wordButtonSplit separates the buttons of a row.
var wordButtonSplit = regexp.MustCompile(`\s{2,}`)

// wordButtonSpan is how far above its buttons the dialog's question may
// sit: its folder path and description wrap at a phone's width.
const wordButtonSpan = 12

// matchWordButtons recognises a word-button question near the bottom of
// the screen. Its title is the first question in the dialog; the answers
// are the buttons' own keys, with no Enter (Enter would press the focused
// button, whichever it is).
func matchWordButtons(lines []string) (Prompt, bool) {
	start := max(0, len(lines)-maxFooter-1)
	for i := len(lines) - 1; i >= start; i-- {
		parts := wordButtonSplit.Split(strings.TrimSpace(lines[i]), -1)
		if len(parts) != 2 {
			continue
		}
		for _, b := range wordButtons {
			if parts[0] != b.yes || parts[1] != b.no {
				continue
			}
			title := ""
			for j := i - 1; j >= max(0, i-wordButtonSpan); j-- {
				l := strings.TrimSpace(lines[j])
				if strings.HasSuffix(l, "?") && headerRe.MatchString(l) {
					title = l
				}
			}
			if title == "" {
				return Prompt{}, false
			}
			return Prompt{
				Kind:  KindConfirm,
				Title: title,
				Options: []wire.Option{
					// Yes runs a turn of the agent's own, which is no
					// permission: the phone picks it on purpose.
					{N: 1, Label: "Yes", Role: wire.RoleOther},
					{N: 2, Label: "No", Role: wire.RoleDeny},
				},
				Keys: map[int][]string{1: {"y"}, 2: {"n"}},
			}, true
		}
	}
	return Prompt{}, false
}
