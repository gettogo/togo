package detect

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// A bullet box is a titled box whose options are plain rows, the one under
// the cursor led by "‣", with the key hints in the box's bottom edge. Amp
// draws its tool approval this way (seen on 0.0.1794312042):
//
//	╭─ Approval Required ─────────────────╮
//	│ Bash:                               │
//	│   echo hello                        │
//	│   Working directory: /Users/jamie/p │
//	│                                     │
//	│ ‣ Allow Once                        │
//	│   Reject with feedback              │
//	│   Allow All for This Session        │
//	│   Allow All for Every Session       │
//	╰─ ↑/↓/j/k move · Enter select · Esc cancel ─╯
//
// Its composer, and an "Up next" list of the prompts queued behind this
// one, sit below the box.

// bulletBoxBelow is how many non-blank lines may follow the box's bottom
// edge: Amp's queue line pair and its four-line composer box.
const bulletBoxBelow = 10

// bulletBoxSpan bounds the box's height.
const bulletBoxSpan = 30

var (
	// bulletRow is an option row: the cursor bullet, or the two spaces in
	// its place, then the label.
	bulletRow = regexp.MustCompile(`^(\s*)‣\s+(\S.*)$`)
	// bottomEdge is a box's bottom edge with words in it.
	bottomEdge = regexp.MustCompile(`^\s*[╰└┗╚][─━═\s]*(\S.*?)[─━═\s]*[╯┘┛╝]\s*$`)
	// scrollBar is the scroll indicator a narrow box draws at its right
	// edge, inside the border.
	scrollBar = regexp.MustCompile(`\s+[█▄▀▌▐░▒▓]\s*$`)
	// boxTool is the tool line that heads the request: "Bash:".
	boxTool = regexp.MustCompile(`^([A-Za-z][\w.-]*):$`)
	// workDir ends the command the request shows.
	workDir = regexp.MustCompile(`^Working directory:`)
)

// matchBulletBox reads a bullet box at the bottom of the screen.
func matchBulletBox(raw []string) (Prompt, bool) {
	bottom := -1
	below := 0
	for i := len(raw) - 1; i >= 0 && below <= bulletBoxBelow; i-- {
		if m := bottomEdge.FindStringSubmatch(raw[i]); m != nil && choiceFooter(m[1]) {
			bottom = i
			break
		}
		if !isFiller(raw[i]) {
			below++
		}
	}
	if bottom < 0 {
		return Prompt{}, false
	}
	top := -1
	title := ""
	for i := bottom - 1; i >= 0 && i >= bottom-bulletBoxSpan; i-- {
		if m := boxTop.FindStringSubmatch(raw[i]); m != nil {
			top, title = i, strings.TrimSpace(m[1])
			break
		}
		if unbox(raw[i]) == raw[i] && strings.TrimSpace(raw[i]) != "" {
			return Prompt{}, false // left the box without meeting its top
		}
	}
	if top < 0 || title == "" {
		return Prompt{}, false
	}
	inner := make([]string, 0, bottom-top-1)
	for _, l := range raw[top+1 : bottom] {
		inner = append(inner, scrollBar.ReplaceAllString(unbox(l), ""))
	}
	// The options: one run of rows at one column, one of them the cursor.
	start, cursor, col := -1, -1, -1
	for i, l := range inner {
		if m := bulletRow.FindStringSubmatch(l); m != nil {
			start, cursor, col = i, i, utf8.RuneCountInString(m[1])
			break
		}
	}
	if cursor < 0 {
		// No options: a field the agent waits at, such as Amp's "Tell Amp
		// what to do differently" after Reject with feedback. The phone is
		// told, with nothing to press.
		return Prompt{Kind: KindWaiting, Title: title, Keys: map[int][]string{}, ReadOnly: true}, true
	}
	labelCol := col + 2
	at := func(l string) (string, bool) {
		if strings.TrimSpace(l) == "" || indentOf(l) != labelCol {
			return "", false
		}
		return strings.TrimSpace(l), true
	}
	for start > 0 {
		if _, ok := at(inner[start-1]); !ok {
			break
		}
		start--
	}
	var labels []string
	end := start
	for i := start; i < len(inner); i++ {
		var label string
		if i == cursor {
			label = strings.TrimSpace(bulletRow.FindStringSubmatch(inner[i])[2])
		} else if l, ok := at(inner[i]); ok {
			label = l
		} else {
			break
		}
		labels = append(labels, label)
		end = i
	}
	for _, l := range inner[end+1:] {
		if strings.TrimSpace(l) != "" {
			return Prompt{}, false // more than options below the list
		}
	}
	if len(labels) < 2 || len(labels) > maxOptions {
		return Prompt{}, false
	}
	p := Prompt{Kind: KindQuestion, Title: title, Keys: map[int][]string{}}
	hasDeny := false
	for i, label := range labels {
		n := i + 1
		role := roleOf(label)
		keys := moveKeys(i - (cursor - start))
		if role == wire.RoleDeny && strings.Contains(strings.ToLower(label), "feedback") {
			// A text field opens for the feedback; Enter leaves it empty.
			keys = append(keys, "Enter")
		}
		hasDeny = hasDeny || role == wire.RoleDeny
		p.Options = append(p.Options, wire.Option{N: n, Label: label, Role: role})
		p.Keys[n] = keys
	}
	if hasDeny && permRe.MatchString(title) || strings.Contains(strings.ToLower(title), "approval") {
		p.Kind = KindPermission
	}
	p.Subject = bulletSubject(inner[:start])
	return p, true
}

// bulletSubject is the command a request shows: the lines under its tool
// line ("Bash:") up to the working directory.
func bulletSubject(lines []string) *wire.Subject {
	var cmd []string
	tool := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case tool == "" && boxTool.MatchString(t):
			tool = boxTool.FindStringSubmatch(t)[1]
		case tool == "":
		case t == "" || workDir.MatchString(t):
			if len(cmd) > 0 || workDir.MatchString(t) {
				return commandSubject(tool, cmd)
			}
		default:
			cmd = append(cmd, t)
		}
	}
	return commandSubject(tool, cmd)
}

func commandSubject(tool string, cmd []string) *wire.Subject {
	if len(cmd) == 0 {
		return nil
	}
	kind := wire.SubjectCommand
	if !strings.EqualFold(tool, "bash") && !strings.EqualFold(tool, "shell") {
		kind = wire.SubjectText
	}
	return &wire.Subject{Kind: kind, Text: strings.Join(cmd, "\n")}
}
