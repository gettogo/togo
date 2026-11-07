package detect

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Sign-in menus: the provider and method pickers of login commands (omp
// and pi /login, droid's Login, clack forms in OpenCode, Kilo and Goose)
// are lists without yes or no, which the general menu shape leaves alone.
// They count when their question is about signing in.

// maxSignInOptions is how many options a sign-in menu may show; provider
// lists run longer than permission menus.
const maxSignInOptions = 12

var (
	signInHeader = regexp.MustCompile(`(?i)\b(log ?in|sign ?in|authenticat\w*|auth method|credentials?|api key)\b`)
	// clackMark leads the active question of a clack or cliclack form.
	clackMark = regexp.MustCompile(`^\s*[◆◇]\s+`)
	// radioLine is one option of a clack select: ● on the chosen one, ○
	// on the rest.
	radioLine = regexp.MustCompile(`^(\s*)([●○◉◯])\s+(\S.*)$`)
	// radioPair is a clack confirm drawn on one line: "● Yes / ○ No".
	radioPair = regexp.MustCompile(`^\s*([●○])\s*(\S[^/]*?)\s*/\s*([●○])\s*(\S.*?)\s*$`)
	// cancelHint is the cancel part of a key-hint footer.
	cancelHint = regexp.MustCompile(`(?i)\b(ctrl\+c|esc(ape)?)\s+(to\s+)?(cancel|quit|exit|back)\b`)
	enterHint  = regexp.MustCompile(`(?i)^(enter|⏎|↵|return)\s+(to\s+)?(continue|confirm|proceed|start|open)\b`)
)

// signInQuestion reports whether a menu's question is about signing in or
// is the active field of a clack form.
func signInQuestion(title string) bool {
	return signInHeader.MatchString(title) || clackMark.MatchString(title)
}

// menuTitle drops box drawing around a question that sits in a dialog's
// top border ("╭─ Select provider to login ─╮") and a form's marker.
func menuTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 0x2500 && r <= 0x257F {
			return ' '
		}
		return r
	}, s)
	s = clackMark.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// cleanLabel drops a scrollbar drawn at the right edge of a list.
func cleanLabel(s string) string {
	return strings.TrimRight(s, " █▐▌│┃▀▄░▒▓")
}

// findRadio finds a clack select: a run of ● and ○ lines at one indent
// with exactly one ●, near the bottom.
func findRadio(lines []string) (menu, bool) {
	c := -1
	for i := len(lines) - 1; i >= 0 && footerLines(lines, i) <= maxFooter+maxSignInOptions; i-- {
		if g := radioLine.FindStringSubmatch(lines[i]); g != nil && (g[2] == "●" || g[2] == "◉") {
			c = i
			break
		}
	}
	if c < 0 {
		return menu{}, false
	}
	indent := radioLine.FindStringSubmatch(lines[c])[1]
	sibling := func(i int) bool {
		g := radioLine.FindStringSubmatch(lines[i])
		return g != nil && g[1] == indent && (g[2] == "○" || g[2] == "◯")
	}
	start, end := c, c
	for start-1 >= 0 && sibling(start-1) {
		start--
	}
	for end+1 < len(lines) && sibling(end+1) {
		end++
	}
	n := end - start + 1
	if n < 2 || n > maxSignInOptions {
		return menu{}, false
	}
	m := menu{start: start, end: end, cursor: c - start}
	for i := start; i <= end; i++ {
		m.labels = append(m.labels, cleanLabel(strings.TrimSpace(radioLine.FindStringSubmatch(lines[i])[3])))
	}
	return m, true
}

// matchRadioPair recognises a clack confirm, "● Yes / ○ No" under its
// question, answered by moving to a side and pressing Enter.
func matchRadioPair(lines []string) (Prompt, bool) {
	i := len(lines) - 1
	g := radioPair.FindStringSubmatch(lines[i])
	if g == nil || g[1] == g[3] || i == 0 {
		return Prompt{}, false
	}
	q := strings.TrimSpace(lines[i-1])
	if !clackMark.MatchString(q) {
		return Prompt{}, false
	}
	left, right := strings.TrimSpace(g[2]), strings.TrimSpace(g[4])
	onLeft := g[1] == "●"
	keys := func(toLeft bool) []string {
		switch {
		case toLeft == onLeft:
			return []string{"Enter"}
		case toLeft:
			return []string{"Left", "Enter"}
		}
		return []string{"Right", "Enter"}
	}
	p := Prompt{Kind: KindQuestion, Title: menuTitle(q), Keys: map[int][]string{1: keys(true), 2: keys(false)}}
	p.Options = []wire.Option{{N: 1, Label: left, Role: roleOf(left)}, {N: 2, Label: right, Role: roleOf(right)}}
	return p, true
}

// matchHintConfirm recognises a question answered by Enter or cancelled,
// as its key-hint footer says ("enter continue • ctrl+c cancel"), when it
// is about signing in: Crush's "we're going to open the browser. Ready?".
func matchHintConfirm(lines []string) (Prompt, bool) {
	n := len(lines)
	if n < 2 {
		return Prompt{}, false
	}
	hint := CleanLine(lines[n-1])
	if !IsKeyHint(hint) || !enterHint.MatchString(strings.TrimSpace(hint)) {
		return Prompt{}, false
	}
	m := cancelHint.FindStringSubmatch(hint)
	if m == nil {
		return Prompt{}, false
	}
	q := menuTitle(CleanLine(lines[n-2]))
	if !strings.HasSuffix(q, "?") {
		return Prompt{}, false
	}
	about := q
	for i := n - 3; i >= 0 && i >= n-5; i-- {
		about += " " + CleanLine(lines[i])
	}
	if !signInHeader.MatchString(about) && !signInHint.MatchString(about) {
		return Prompt{}, false
	}
	cancel := "C-c"
	if strings.HasPrefix(strings.ToLower(m[1]), "esc") {
		cancel = "Escape"
	}
	g := enterHint.FindStringSubmatch(strings.TrimSpace(hint))
	verb := strings.ToUpper(g[3][:1]) + g[3][1:]
	return Prompt{
		Kind:    KindQuestion,
		Title:   q,
		Options: []wire.Option{{N: 1, Label: verb, Role: wire.RoleApprove}, {N: 2, Label: "Cancel", Role: wire.RoleDeny}},
		Keys:    map[int][]string{1: {"Enter"}, 2: {cancel}},
	}, true
}
