package detect

import (
	"regexp"
	"strings"
	"unicode/utf8"

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

// crushButtons is the button row of Crush 0.100's permission dialog, a box
// drawn over the chat (so the chat's text runs on either side of it):
// "Permission Required", the tool, path and command, then
// "Allow      Allow for Session      Deny" over "←/→ choose • enter confirm".
// Its keys are letters (a allows, s allows for the session, d denies);
// Enter would press whichever button has the focus, which the capture does
// not show.
var crushButtons = regexp.MustCompile(`│\s*Allow\s{2,}Allow for Session\s{2,}Deny\s*│`)

// crushField is a labelled row of the dialog ("Tool bash", "Path …").
var crushField = regexp.MustCompile(`^ (Tool|Path|Desc|File|URL|Command) `)

// crushSpan is how far above its buttons the dialog's title may sit.
const crushSpan = 16

// matchCrushPermission recognises Crush's permission dialog near the bottom
// of the screen. Only the text inside the box is read.
func matchCrushPermission(raw []string) (Prompt, bool) {
	start := max(0, len(raw)-crushSpan-maxFooter*3)
	for i := len(raw) - 1; i >= start; i-- {
		loc := crushButtons.FindStringIndex(raw[i])
		if loc == nil {
			continue
		}
		col := utf8.RuneCountInString(raw[i][:loc[0]])
		// inside is a row's text between the box's edges, untrimmed.
		inside := func(l string) (string, bool) {
			r := []rune(l)
			if col >= len(r) || r[col] != '│' {
				return "", false
			}
			rest := r[col+1:]
			for j, c := range rest {
				if c == '│' {
					return strings.TrimRight(string(rest[:j]), " "), true
				}
			}
			return "", false
		}
		top := -1
		for j := i - 1; j >= max(0, i-crushSpan) && top < 0; j-- {
			if strings.Contains(raw[j], "Permission Required") {
				top = j
			}
		}
		if top < 0 {
			return Prompt{}, false
		}
		// Labelled fields ("Tool bash", "Path …" and its wrapped
		// lines, "Desc …"), then after a blank row the command itself.
		var tool, command string
		fields, blank := false, false
		for j := top + 1; j < i; j++ {
			t, ok := inside(raw[j])
			switch {
			case !ok:
			case strings.TrimSpace(t) == "":
				blank = fields
			case crushField.MatchString(t):
				fields, blank = true, false
				if strings.HasPrefix(t, " Tool ") {
					tool = strings.TrimSpace(strings.TrimPrefix(t, " Tool "))
				}
			case blank && command == "":
				command = strings.TrimSpace(t)
			}
		}
		title := "Permission Required"
		if tool != "" {
			title = "Allow " + tool + "?"
		}
		p := Prompt{
			Kind:  KindPermission,
			Title: title,
			Options: []wire.Option{
				{N: 1, Label: "Allow", Role: wire.RoleApprove},
				{N: 2, Label: "Allow for Session", Role: wire.RoleApproveAlways},
				{N: 3, Label: "Deny", Role: wire.RoleDeny},
			},
			Keys: map[int][]string{1: {"a"}, 2: {"s"}, 3: {"d"}},
		}
		if command != "" {
			p.Subject = NewSubject(SubjectKindForTool(tool), command)
		}
		return p, true
	}
	return Prompt{}, false
}
