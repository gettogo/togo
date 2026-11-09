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
// CLI draws a radio dot ("● 1. Allow once"); the dot is not a
// cursor elsewhere, since agents also use it as a bullet for their output.
const numberedCursorRunes = cursorRunes + "●"

var (
	ynToken = regexp.MustCompile(`(?i)[\[(]\s*(y|yes|\(y\)es)\s*[/,]\s*(n|no|\(n\)o)\s*[\])]\s*[:?]?\s*$`)
	// aiderYN is Aider's line-mode question: "(Y)es/(N)o [Yes]:", with
	// "/(A)ll/(S)kip all" or "/(D)on't ask again" before the default.
	aiderDefault = regexp.MustCompile(`^\s*\[(Yes|No)\]:\s*$`)
	aiderYN      = regexp.MustCompile(`\(Y\)es/\(N\)o(/\([A-Z]\)[^/\[]*)*\s*\[(Yes|No)\]:\s*$`)
	numbered     = regexp.MustCompile(`^\s*([` + numberedCursorRunes + `]\s*)?(\d{1,2})[.)]\s+(\S.*)$`)
	cursorLine   = regexp.MustCompile(`^(\s*)[` + cursorRunes + `]\s+(\S.*)$`)
	shortcut     = regexp.MustCompile(`(?i)\s+\((esc|[a-z0-9])\)$`)
	// leadShortcut is a key shown before the label: Cursor Agent's
	// "▶ [a] Trust this workspace" / "[q] Quit".
	leadShortcut = regexp.MustCompile(`^\[([a-z])\]\s+(\S.*)$`)
	// bracketNum is a "[1] label" option: Auggie's workspace-indexing
	// chooser, which draws no cursor glyph and selects on the digit.
	bracketNum = regexp.MustCompile(`^\s*([` + cursorRunes + `]\s*)?\[(\d)\]\s+(\S.*)$`)
	// selectHint is a footer that says the digits or Enter pick an option.
	selectHint = regexp.MustCompile(`(?i)\b(select|confirm|choose)\b`)
	// trustTitle is a folder trust question.
	trustTitle = regexp.MustCompile(`(?i)\btrust\b`)
	// trustGrant is the option that grants it.
	quitRe     = regexp.MustCompile(`(?i)^(quit|exit)\b`)
	trustGrant = regexp.MustCompile(`(?i)^(yes,? )?(i )?trust\b`)
	headerRe   = regexp.MustCompile(`(?i)^(do you want|would you like|allow|approve|run|permission|proceed|confirm|apply|choose an option|select an option)\b`)
	permRe     = regexp.MustCompile(`(?i)\b(allow|approve|permission|run|execute|proceed|edit|write|create|delete|apply|command|make this|make the following|edits?|changes?|patch)\b`)
	denyRe     = regexp.MustCompile(`(?i)^(no|reject|deny|decline|cancel|abort|skip|continue without)\b`)
	approveRe  = regexp.MustCompile(`(?i)^(yes|allow|always|approve|accept|run|proceed|continue|ok|okay|apply|confirm|trust|remember)\b`)
	alwaysRe   = regexp.MustCompile(`(?i)(always|don't ask|do not ask|never ask|for this session|for the session|auto-accept|all edits|remember|permanent|in this project|for this project|globally|trust)`)
)

// opensBrowser matches a y/n question whose Yes opens a URL, a browser or a
// sign-in: Aider's "Open documentation url for more info?" and "Login to
// OpenRouter or create a free account?".
var opensBrowser = regexp.MustCompile(`(?i)\b(open|visit|launch)\b.*\b(url|link|browser|page|docs|documentation|website)\b|\b(log ?in|sign ?in|sign ?up|create (a |an )?(free )?account)\b`)

// matchConfirm recognises a line-mode y/n question on the last line, where
// the terminal cursor sits while a shell tool waits for input. The answer
// is the letter followed by Enter: these prompts read a whole line, and
// single-keypress prompts accept the trailing Enter as their own default
// confirmation or as an empty line, which does not change the answer.
func matchConfirm(lines []string) (Prompt, bool) {
	last := lines[len(lines)-1]
	if len(lines) >= 2 && aiderDefault.MatchString(last) {
		// An 80-column pane wraps Aider's "[Yes]:" onto its own line (seen
		// live on 0.86.2): read the two lines as one.
		lines = append(lines[:len(lines)-2:len(lines)-2], strings.TrimRight(lines[len(lines)-2], " ")+" "+strings.TrimSpace(last))
		last = lines[len(lines)-1]
	}
	loc := ynToken.FindStringIndex(last)
	if loc == nil {
		loc = aiderYN.FindStringIndex(last)
	}
	if loc == nil && len(lines) >= 2 && utf8.RuneCountInString(strings.TrimSpace(last)) <= 8 {
		// A full-width line wraps the token itself: Gemini CLI's "Do you
		// want to continue? [Y" then "/n]: " at 100 columns (seen live).
		joined := strings.TrimRight(lines[len(lines)-2], " ") + strings.TrimSpace(last)
		if l := ynToken.FindStringIndex(joined); l != nil {
			lines = append(lines[:len(lines)-2:len(lines)-2], joined)
			last, loc = joined, l
		}
	}
	if loc == nil && len(lines) >= 2 {
		// A narrow pane wraps inside the token anywhere: Aider at a
		// phone's 47 columns left "… free account? (" above
		// "Y)es/(N)o [Yes]:" (seen live), and every chat prompt was typed
		// into the question. The wrap cuts mid-word, so join with no space;
		// the token must straddle the cut, else the last line alone matched.
		joined := strings.TrimRight(lines[len(lines)-2], " ") + strings.TrimSpace(last)
		l := ynToken.FindStringIndex(joined)
		if l == nil {
			l = aiderYN.FindStringIndex(joined)
		}
		if l != nil {
			lines = append(lines[:len(lines)-2:len(lines)-2], joined)
			last, loc = joined, l
		}
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
	yes := wire.RoleApprove
	if opensBrowser.MatchString(title) {
		// A Yes that opens a page or a sign-in on the Mac is never a
		// one-tap approve: the phone must pick it on purpose.
		yes = wire.RoleOther
	}
	return Prompt{
		Kind:  KindConfirm,
		Title: title,
		Options: []wire.Option{
			{N: 1, Label: "Yes", Role: yes},
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
	// arrows marks a numbered list that does not take its digits (Codex
	// 0.167's folder trust at start ignores them): the cursor moves.
	arrows bool
	// escNo marks a scrolled list cut to its first option: Esc, which
	// its hint offers, is the No it cannot show.
	escNo bool
}

func matchMenu(lines []string, boxed []bool) (Prompt, bool) {
	if p, ok := matchRadioPair(lines); ok {
		return p, true
	}
	m, ok := findNumbered(lines)
	if !ok {
		m, ok = findBracketed(lines)
	}
	if !ok {
		m, ok = findArrow(lines)
	}
	if !ok {
		m, ok = findRadio(lines)
	}
	if !ok {
		return Prompt{}, false
	}
	for _, l := range m.labels {
		if composerHint.MatchString(l) {
			// The agent's input box and its status lines, under the
			// cursor-led placeholder ("› Ask Codex to do anything").
			return Prompt{}, false
		}
	}
	h, title, titled := borderTitle(lines, boxed, m.start)
	if !titled {
		if h, ok = findHeader(lines, m.start); !ok {
			return Prompt{}, false
		}
		title = strings.TrimSpace(lines[h])
	}
	if leadQuestion(title) != "" && !m.arrows {
		// Only Codex 0.167's start-up dialogs are read this way: their
		// keys are known (the arrows). Codex 0.144's trust question is
		// left to the terminal, as before.
		return Prompt{}, false
	}
	if !titled && h > 0 && wrappedHeader(lines[h-1], title) {
		// A narrow pane wrapped the question: "Do you trust the contents
		// of this" / "directory?" (Cursor Agent at 47 columns).
		title = strings.TrimSpace(lines[h-1]) + " " + title
		h--
	}
	if echoLine(title) {
		// The header was the wrapped tail of a transcript line.
		return Prompt{}, false
	}
	if i := strings.Index(title, "? ("); i >= 0 {
		title = title[:i+1] // the question, without the aside after it
	}
	if q := leadQuestion(title); q != "" {
		title = q // the question, without the description after it
	}
	signIn := signInQuestion(title)
	if footerLines(lines, m.end) > maxFooter && !(signIn && footerLines(lines, m.end) <= maxFooter+maxSignInOptions) {
		return Prompt{}, false
	}
	if len(m.labels) > maxOptions && !signIn {
		return Prompt{}, false
	}
	p := Prompt{Title: title, Keys: map[int][]string{}}
	if signIn {
		// The lines between a login menu's question and its options are a
		// search field and group names, not what the question is about.
		p.Title = menuTitle(title)
	} else {
		p.Detail = detail(lines, boxed, h, m.start)
		p.Subject = subjectOf(lines, boxed, h, m.start)
		if p.Subject == nil {
			p.Subject = pathSubject(p.Detail)
		}
	}
	hasRole := false
	for i, l := range m.labels {
		n := i + 1
		label, key := splitShortcut(cleanLabel(l))
		role := roleOf(label)
		if role != wire.RoleOther {
			hasRole = true
		}
		p.Options = append(p.Options, wire.Option{N: n, Label: label, Role: role})
		p.Keys[n] = keysFor(m, i, key)
	}
	if m.escNo {
		p.Options = append(p.Options, wire.Option{N: len(p.Options) + 1, Label: "No", Role: wire.RoleDeny})
		p.Keys[len(p.Options)] = []string{"Escape"}
	}
	trustOneTap(&p)
	// An unnumbered list with a cursor is also how pickers and file
	// browsers look; only treat it as a prompt when it offers an answer,
	// or asks how to sign in.
	if !m.numbered && !hasRole && !signIn {
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
	// The last option's own wrapped lines sit below it.
	var tail []string
	for j := b + 1; j < len(lines) && len(tail) < maxWrap && continues(lines[b], lines[j]) && !menuHint.MatchString(lines[j]); j++ {
		tail = append(tail, strings.TrimSpace(lines[j]))
		m.end = j
	}
	want := 0
	var wrapped []string
	for i := b; i >= 0; i-- {
		g := numbered.FindStringSubmatch(lines[i])
		if g == nil {
			if len(wrapped) < maxWrap && wrapsOption(lines, i) {
				wrapped = append([]string{strings.TrimSpace(lines[i])}, wrapped...)
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
		if i == b && len(tail) > 0 {
			label += " " + strings.Join(tail, " ")
		}
		if len(wrapped) > 0 {
			label += " " + strings.Join(wrapped, " ")
			wrapped = nil
		}
		m.labels = append([]string{label}, m.labels...)
		m.start = i
		if n == 1 {
			break
		}
	}
	if len(wrapped) > 0 {
		return menu{}, false
	}
	m.arrows = arrowHinted(lines, m.end)
	if want == 1 && m.cursor == 0 && len(m.labels) == 1 && scrolledEsc(lines, m.end) {
		// A short pane shows only the first option of a list it scrolls
		// (Copilot CLI's folder trust and run-command questions in 24
		// rows): it, and Esc, which cancels.
		m.escNo = true
		return m, true
	}
	if want != 1 || m.cursor < 0 || len(m.labels) < 2 || len(m.labels) > maxOptions {
		return menu{}, false
	}
	return m, true
}

// menuHint is a menu's key-hint footer, which no option wraps into.
var menuHint = regexp.MustCompile(`(?i)\b(enter|esc|↑|↓)\b.*\b(confirm|cancel|select|navigate|continue|back|skip)\b|↑/↓`)

// arrowHint is Codex 0.167's start-up dialog footer ("enter continue · esc
// back", "enter confirm · esc skip"): its lists move by arrow keys.
var arrowHint = regexp.MustCompile(`(?i)^enter (continue|confirm) · esc \w+`)

// arrowHinted reports that footer in the few lines below a menu.
func arrowHinted(lines []string, end int) bool {
	for j := end + 1; j < len(lines) && j <= end+maxFooter; j++ {
		if arrowHint.MatchString(strings.TrimSpace(lines[j])) {
			return true
		}
	}
	return false
}

// scrollHint is a footer that says the list scrolls and Esc cancels it.
var scrollHint = regexp.MustCompile(`(?i)\bto scroll\b.*\besc to cancel\b`)

// scrolledEsc reports a scroll-and-cancel hint in the few lines below a
// menu.
func scrolledEsc(lines []string, end int) bool {
	for j := end + 1; j < len(lines) && j <= end+maxFooter; j++ {
		if scrollHint.MatchString(lines[j]) {
			return true
		}
	}
	return false
}

// maxWrap bounds how many wrapped lines one option label may span below
// its numbered line: a long path in Claude's "always allow access to …"
// option wraps over three lines on a narrow pane.
const maxWrap = 4

// wrapsOption reports whether line i continues a numbered option above it:
// it and every line between sit at the label column of that option.
func wrapsOption(lines []string, i int) bool {
	for j := i - 1; j >= 0 && j >= i-maxWrap; j-- {
		if numbered.MatchString(lines[j]) {
			return continues(lines[j], lines[i])
		}
		if indentOf(lines[j]) != indentOf(lines[i]) || isFiller(lines[j]) {
			return false
		}
	}
	return false
}

func indentOf(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

// continues reports whether next is a wrapped second line of the numbered
// option on line prev: indented to exactly where prev's label starts, or,
// on an option without the cursor, to where its number starts (Copilot CLI
// 1.0.107 wraps "2. Yes, and don't ask again for `touch` in this
// directory (…)" back to the number's column; seen live at 80 and 47
// columns, where every approval was left to the terminal).
func continues(prev, next string) bool {
	g := numbered.FindStringSubmatchIndex(prev)
	if g == nil || isFiller(next) || numbered.MatchString(next) {
		return false
	}
	indent := len(next) - len(strings.TrimLeft(next, " "))
	if indent == utf8.RuneCountInString(prev[:g[6]]) {
		return true
	}
	return g[2] < 0 && indent == indentOf(prev)
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
	if n < 2 || n > maxSignInOptions {
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

// echoLine reports a transcript line: a sent prompt led by a prompt
// marker, or the agent's reply led by its bullet. Neither is a dialog's
// question, whatever words it holds.
func echoLine(t string) bool {
	r, size := utf8.DecodeRuneInString(t)
	if r == '•' || r == '⏺' {
		// The agent's own reply ("• I'll find the login handler…").
		return len(t) > size && t[size] == ' '
	}
	return (r == '›' || r == '❯') && len(t) > size && t[size] == ' ' && utf8.RuneCountInString(t) > 40
}

// composerHint is a status or key-hint line under an agent's input box,
// which no menu option reads like.
var composerHint = regexp.MustCompile(`(?i)\bcontext \d+% left\b|\? for shortcuts\b|\besc again to edit\b`)

// titleRe names dialogs whose title is no question: Codex 0.167's
// "Hooks need review" at start, over its new or changed hooks.
var titleRe = regexp.MustCompile(`(?i)^hooks need review$`)

// leadQuestion is the question a line opens with when a description runs
// on after it: Codex 0.167's "Trust this folder? Codex can read, edit…".
// Only a trust or permission question counts, so prose that asks
// something in passing is no header.
func leadQuestion(t string) string {
	q, rest, ok := strings.Cut(t, "? ")
	if !ok || rest == "" || utf8.RuneCountInString(q) > 60 || utf8.RuneCountInString(q) < 3 {
		return ""
	}
	if !trustTitle.MatchString(q) && !headerRe.MatchString(q) {
		return ""
	}
	return q + "?"
}

func findHeader(lines []string, start int) (int, bool) {
	// A trust question that opens a paragraph is the header, though the
	// paragraph's wrapped lines below it may start like one ("permission
	// settings. …" at a phone's width).
	gap := 0
	for i := start - 1; i >= 0 && gap <= maxHeaderGap; i-- {
		if isFiller(lines[i]) {
			continue
		}
		if leadQuestion(strings.TrimSpace(lines[i])) != "" {
			return i, true
		}
		gap++
	}
	gap = 0
	for i := start - 1; i >= 0 && gap <= maxHeaderGap; i-- {
		if isFiller(lines[i]) {
			continue
		}
		t := strings.TrimSpace(lines[i])
		if echoLine(t) {
			// The user's own prompt as the agent echoes it ("› POST
			// /login crashes …" in Codex) asks nothing, though its words
			// may read like a sign-in question.
			gap++
			continue
		}
		if isHeader(t) {
			return i, true
		}
		gap++
	}
	return 0, false
}

// isHeader reports a line that reads as a dialog's question or title.
func isHeader(t string) bool {
	return strings.HasSuffix(t, "?") || strings.Contains(t, "? (") || headerRe.MatchString(t) || signInQuestion(t) ||
		leadQuestion(t) != "" || titleRe.MatchString(t)
}

// titledTop is a dialog's top border with its title set in it: omp 18.16's
// "╭─ Allow tool: bash ─────╮".
var titledTop = regexp.MustCompile(`^\s*[╭┌┏╔][─━═]+\s+(\S.*?)\s+[─━═]+[╮┐┓╗]$`)

// borderTitle finds the title in the top border of the box a menu sits
// in, however much the box holds above the options (omp's command, wrapped
// over several lines at a phone's width). It returns the border's index
// and the title when the menu is boxed and that title reads as a header.
func borderTitle(lines []string, boxed []bool, start int) (int, string, bool) {
	if start >= len(lines) || !boxed[start] {
		return 0, "", false
	}
	for i := start - 1; i >= 0; i-- {
		if boxed[i] {
			continue
		}
		g := titledTop.FindStringSubmatch(lines[i])
		if g == nil || !isHeader(g[1]) {
			return 0, "", false
		}
		return i, g[1], true
	}
	return 0, "", false
}

// detail is what the prompt is about: the lines between the question and
// the options, or failing that the few lines above the question inside the
// same boxed dialog. Unboxed lines above a question are agent output, which
// would make the fingerprint change with unrelated scrollback.
func detail(lines []string, boxed []bool, header, start int) *string {
	var d []string
	for i := header + 1; i < start; i++ {
		if !isFiller(lines[i]) && !IsKeyHint(CleanLine(lines[i])) {
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
	if g := leadShortcut.FindStringSubmatch(label); g != nil {
		return strings.TrimSpace(g[2]), strings.ToLower(g[1])
	}
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
	// Cursor CLI 2026.11: "Run this command?" → "Run (once) (y)".
	"run (once)": true, "allow (once)": true,
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
	if m.numbered && !m.arrows {
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

// findBracketed finds a run of "[1] … [2] …" options numbered from 1,
// with or without a cursor, above a footer that says how to select.
// Lines between options that are indented and not options themselves are
// the label wrapped at a narrow pane (Auggie at 47 columns).
func findBracketed(lines []string) (menu, bool) {
	b := -1
	for i := len(lines) - 1; i >= 0 && footerLines(lines, i) <= maxFooter; i-- {
		if bracketNum.MatchString(lines[i]) {
			b = i
			break
		}
	}
	if b < 0 {
		return menu{}, false
	}
	hinted := false
	for _, l := range lines[b+1:] {
		if selectHint.MatchString(l) {
			hinted = true
		}
	}
	m := menu{end: b, numbered: true}
	want := 0
	var wrapped []string
	for i := b; i >= 0; i-- {
		g := bracketNum.FindStringSubmatch(lines[i])
		if g == nil {
			if len(wrapped) < maxWrap && indentOf(lines[i]) > 0 && !isFiller(lines[i]) {
				wrapped = append([]string{strings.TrimSpace(lines[i])}, wrapped...)
				continue
			}
			return menu{}, false
		}
		n, _ := strconv.Atoi(g[2])
		if want != 0 && n != want-1 {
			return menu{}, false
		}
		want = n
		if g[1] != "" {
			m.cursor = n - 1
			hinted = true
		}
		label := strings.TrimSpace(g[3])
		if len(wrapped) > 0 {
			label += " " + strings.Join(wrapped, " ")
			wrapped = nil
		}
		m.labels = append([]string{label}, m.labels...)
		m.start = i
		if n == 1 {
			break
		}
	}
	if want != 1 || !hinted || len(m.labels) < 2 || len(m.labels) > maxOptions {
		return menu{}, false
	}
	return m, true
}

// wrappedHeader reports a question line that is the tail of the line
// above: it starts lower case and the line above ends mid-sentence.
func wrappedHeader(prev, title string) bool {
	prev = strings.TrimSpace(prev)
	if prev == "" || title == "" || isFiller(prev) || strings.ContainsAny(prev[len(prev)-1:], ".?!:") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(title)
	return r >= 'a' && r <= 'z'
}

// trustOneTap makes a folder trust question answerable with one tap: when
// nothing else approves, its single trust option is the approval. Trust is
// the only grant such a question offers, so approve does not widen it.
// Seen on Cursor Agent 2026.11.05 ("[a] Trust this workspace").
func trustOneTap(p *Prompt) {
	if !trustTitle.MatchString(p.Title) {
		return
	}
	idx := -1
	for i, o := range p.Options {
		switch {
		case o.Role == wire.RoleApprove:
			return
		case trustGrant.MatchString(o.Label):
			if idx >= 0 {
				return
			}
			idx = i
		}
	}
	if idx >= 0 {
		p.Options[idx].Role = wire.RoleApprove
	}
	for i, o := range p.Options {
		if o.Role == wire.RoleOther && quitRe.MatchString(o.Label) {
			// Leaving a trust question declines it.
			p.Options[i].Role = wire.RoleDeny
		}
	}
}
