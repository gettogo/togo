package detect

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// maxFooter is how many non-border lines may follow a menu (key hints,
	// a status bar). More than that and the menu is not what the screen is
	// currently showing.
	maxFooter = 4
	// maxHeaderGap is how far above a menu its question may be: the
	// command or file being asked about usually sits between them.
	maxHeaderGap = 6
	maxOptions   = 9
	maxDetail    = 3
)

const cursorRunes = "❯›>▶➤→"

// numberedCursorRunes also mark the chosen line of a numbered menu. Gemini
// CLI and Qwen Code draw a radio dot ("● 1. Allow once"); the dot is not a
// cursor elsewhere, since agents also use it as a bullet for their output.
const numberedCursorRunes = cursorRunes + "●"

var (
	ynToken = regexp.MustCompile(`(?i)[\[(]\s*(y|yes)\s*/\s*(n|no)\s*[\])]\s*[:?]?\s*$`)
	// aiderYN is Aider's line-mode question: "(Y)es/(N)o [Yes]:", with
	// "/(A)ll/(S)kip all" or "/(D)on't ask again" before the default.
	aiderYN    = regexp.MustCompile(`\(Y\)es/\(N\)o(/\([A-Z]\)[^/\[]*)*\s*\[(Yes|No)\]:\s*$`)
	numbered   = regexp.MustCompile(`^\s*([` + numberedCursorRunes + `]\s*)?(\d{1,2})[.)]\s+(\S.*)$`)
	cursorLine = regexp.MustCompile(`^(\s*)[` + cursorRunes + `]\s+(\S.*)$`)
	shortcut   = regexp.MustCompile(`(?i)\s+\((esc|[a-z0-9])\)$`)
	headerRe   = regexp.MustCompile(`(?i)^(do you want|would you like|allow|approve|run|permission|proceed|confirm|apply)\b`)
	permRe     = regexp.MustCompile(`(?i)\b(allow|approve|permission|run|execute|proceed|edit|write|create|delete|apply|command|make this|make the following|edits?|changes?|patch)\b`)
	denyRe     = regexp.MustCompile(`(?i)^(no|reject|deny|decline|cancel|abort|skip)\b`)
	approveRe  = regexp.MustCompile(`(?i)^(yes|allow|always|approve|accept|run|proceed|continue|ok|okay|apply|confirm|trust|remember)\b`)
	alwaysRe   = regexp.MustCompile(`(?i)(always|don't ask|do not ask|never ask|for this session|for the session|auto-accept|all edits|remember|permanent|in this project|for this project|globally|trust)`)
)

// matchConfirm recognises a line-mode y/n question on the last line, where
// the terminal cursor sits while a shell tool waits for input. The answer
// is the letter followed by Enter: these prompts read a whole line, and
// single-keypress prompts accept the trailing Enter as their own default
// confirmation or as an empty line, which does not change the answer.
func matchConfirm(lines []string) (Prompt, bool) {
	last := lines[len(lines)-1]
	loc := ynToken.FindStringIndex(last)
	if loc == nil {
		loc = aiderYN.FindStringIndex(last)
	}
	if loc == nil {
		return Prompt{}, false
	}
	title := strings.TrimSpace(last[:loc[0]])
	if title == "" {
		if len(lines) < 2 {
			return Prompt{}, false
		}
		title = strings.TrimSpace(lines[len(lines)-2])
	}
	return Prompt{
		Kind:  KindConfirm,
		Title: title,
		Options: []wire.Option{
			{N: 1, Label: "Yes", Role: wire.RoleApprove},
			{N: 2, Label: "No", Role: wire.RoleDeny},
		},
		Keys: map[int][]string{1: {"y", "Enter"}, 2: {"n", "Enter"}},
	}, true
}

// menu is a block of options found on screen, as indexes into the lines.
type menu struct {
	start, end int // inclusive line range of the options
	cursor     int // option index (0-based) under the cursor
	labels     []string
	numbered   bool
}

func matchMenu(lines []string, boxed []bool) (Prompt, bool) {
	m, ok := findNumbered(lines)
	if !ok {
		m, ok = findArrow(lines)
	}
	if !ok || footerLines(lines, m.end) > maxFooter {
		return Prompt{}, false
	}
	h, ok := findHeader(lines, m.start)
	if !ok {
		return Prompt{}, false
	}
	p := Prompt{Title: strings.TrimSpace(lines[h]), Keys: map[int][]string{}}
	p.Detail = detail(lines, boxed, h, m.start)
	p.Subject = subjectOf(lines, boxed, h, m.start)
	hasRole := false
	for i, l := range m.labels {
		n := i + 1
		label, key := splitShortcut(l)
		role := roleOf(label)
		if role != wire.RoleOther {
			hasRole = true
		}
		p.Options = append(p.Options, wire.Option{N: n, Label: label, Role: role})
		p.Keys[n] = keysFor(m, i, key)
	}
	// An unnumbered list with a cursor is also how pickers and file
	// browsers look; only treat it as a prompt when it offers an answer.
	if !m.numbered && !hasRole {
		return Prompt{}, false
	}
	p.Kind = KindQuestion
	if permRe.MatchString(p.Title) {
		p.Kind = KindPermission
	}
	return p, true
}

// findNumbered finds the lowest run of "1. ... 2. ..." lines in the footer
// region, numbered from 1 with exactly one cursor-marked line.
func findNumbered(lines []string) (menu, bool) {
	b := -1
	for i := len(lines) - 1; i >= 0 && footerLines(lines, i) <= maxFooter; i-- {
		if numbered.MatchString(lines[i]) {
			b = i
			break
		}
	}
	if b < 0 {
		return menu{}, false
	}
	m := menu{end: b, cursor: -1, numbered: true}
	want := 0
	wrapped := ""
	for i := b; i >= 0; i-- {
		g := numbered.FindStringSubmatch(lines[i])
		if g == nil {
			if wrapped == "" && i > 0 && continues(lines[i-1], lines[i]) {
				wrapped = strings.TrimSpace(lines[i])
				continue
			}
			break
		}
		n, _ := strconv.Atoi(g[2])
		if want != 0 && n != want-1 {
			break
		}
		want = n
		if g[1] != "" {
			if m.cursor >= 0 {
				return menu{}, false
			}
			m.cursor = n - 1
		}
		label := strings.TrimSpace(g[3])
		if wrapped != "" {
			label += " " + wrapped
			wrapped = ""
		}
		m.labels = append([]string{label}, m.labels...)
		m.start = i
		if n == 1 {
			break
		}
	}
	if wrapped != "" {
		return menu{}, false
	}
	if want != 1 || m.cursor < 0 || len(m.labels) < 2 || len(m.labels) > maxOptions {
		return menu{}, false
	}
	return m, true
}

// continues reports whether next is a wrapped second line of the numbered
// option on line prev: indented to exactly where prev's label starts.
func continues(prev, next string) bool {
	g := numbered.FindStringSubmatchIndex(prev)
	if g == nil || isFiller(next) || numbered.MatchString(next) {
		return false
	}
	col := utf8.RuneCountInString(prev[:g[6]])
	return len(next)-len(strings.TrimLeft(next, " ")) == col
}

// findArrow finds an unnumbered list where one line carries the cursor and
// its siblings are aligned with the cursor line's label.
func findArrow(lines []string) (menu, bool) {
	c := -1
	var col int
	for i := len(lines) - 1; i >= 0 && footerLines(lines, i) <= maxFooter+maxOptions; i-- {
		if g := cursorLine.FindStringSubmatchIndex(lines[i]); g != nil {
			c = i
			col = utf8.RuneCountInString(lines[i][:g[4]])
			break
		}
	}
	if c < 0 {
		return menu{}, false
	}
	aligned := func(i int) bool {
		l := lines[i]
		if cursorLine.MatchString(l) || numbered.MatchString(l) {
			return false
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		return indent == col && indent > 0
	}
	start, end := c, c
	for start-1 >= 0 && aligned(start-1) {
		start--
	}
	for end+1 < len(lines) && aligned(end+1) {
		end++
	}
	n := end - start + 1
	if n < 2 || n > maxOptions {
		return menu{}, false
	}
	m := menu{start: start, end: end, cursor: c - start}
	for i := start; i <= end; i++ {
		l := lines[i]
		if i == c {
			l = cursorLine.FindStringSubmatch(l)[2]
		}
		m.labels = append(m.labels, strings.TrimSpace(l))
	}
	return m, true
}

// footerLines counts the non-border lines below index i.
func footerLines(lines []string, i int) int {
	n := 0
	for _, l := range lines[i+1:] {
		if !isFiller(l) {
			n++
		}
	}
	return n
}

func findHeader(lines []string, start int) (int, bool) {
	gap := 0
	for i := start - 1; i >= 0 && gap <= maxHeaderGap; i-- {
		if isFiller(lines[i]) {
			continue
		}
		t := strings.TrimSpace(lines[i])
		if strings.HasSuffix(t, "?") || headerRe.MatchString(t) {
			return i, true
		}
		gap++
	}
	return 0, false
}

// detail is what the prompt is about: the lines between the question and
// the options, or failing that the few lines above the question inside the
// same boxed dialog. Unboxed lines above a question are agent output, which
// would make the fingerprint change with unrelated scrollback.
func detail(lines []string, boxed []bool, header, start int) *string {
	var d []string
	for i := header + 1; i < start; i++ {
		if !isFiller(lines[i]) {
			d = append(d, strings.TrimSpace(lines[i]))
		}
	}
	if len(d) == 0 && boxed[header] {
		for i := header - 1; i >= 0 && len(d) < maxDetail && boxed[i] && !isBorder(lines[i]); i-- {
			if t := strings.TrimSpace(lines[i]); t != "" {
				d = append([]string{t}, d...)
			}
		}
	}
	if len(d) == 0 {
		return nil
	}
	s := strings.Join(d, "\n")
	return &s
}

// splitShortcut strips a trailing single-key hint such as "(y)" or "(esc)"
// and returns the tmux key name it names.
func splitShortcut(label string) (string, string) {
	g := shortcut.FindStringSubmatch(label)
	if g == nil {
		return label, ""
	}
	key := strings.ToLower(g[1])
	if key == "esc" {
		key = "Escape"
	}
	return strings.TrimSpace(label[:len(label)-len(g[0])]), key
}

// plainApprove lists the only labels that grant a single use. A remote
// "approve" must never widen a grant, so any qualified yes is classified
// as approve_always (when it names a lasting grant) or other, and the phone
// can still pick it explicitly by option number.
var plainApprove = map[string]bool{
	"yes": true, "allow": true, "approve": true, "run": true,
	"yes, proceed": true, "allow once": true, "approve once": true,
	"run once": true, "yes, once": true, "yes, allow once": true,
}

func roleOf(label string) string {
	norm := strings.ToLower(strings.TrimRight(normalise(label), ".!"))
	switch {
	case denyRe.MatchString(label):
		return wire.RoleDeny
	case plainApprove[norm]:
		return wire.RoleApprove
	case alwaysRe.MatchString(label):
		return wire.RoleApproveAlways
	}
	return wire.RoleOther
}

// keysFor picks the keys for option i. A shortcut the TUI advertises wins;
// numbered menus select on the digit; unnumbered menus need the cursor
// moved from where it is now, then Enter.
func keysFor(m menu, i int, key string) []string {
	if key != "" {
		return []string{key}
	}
	if m.numbered {
		return strings.Split(strconv.Itoa(i+1), "")
	}
	var keys []string
	for d := i - m.cursor; d != 0; {
		if d > 0 {
			keys = append(keys, "Down")
			d--
		} else {
			keys = append(keys, "Up")
			d++
		}
	}
	return append(keys, "Enter")
}
