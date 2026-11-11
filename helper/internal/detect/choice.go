package detect

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Choice is a selection dialog as TUIs draw it: a prompt, then a list of
// options each marked with a radio (○ ●), a check box (☐ ☑, [ ] [x]) or
// a cursor (❯), with an optional description line indented under it, an
// "Other (type your own)" row, a tab strip when several questions are
// asked in turn, a Review page whose one row is Submit, and a key-hint
// footer ("⏎ select · n note · ↑/↓ move · ⎋ cancel"). omp's ask tool draws
// exactly this (seen on 18.16.0); clack-based agents draw the radio form.
//
// The footer is the cue that the list is being asked, not shown: a list
// without one is a transcript's bullet points or a tool call's preview of
// the same options, which omp prints above its dialog.
type Choice struct {
	Prompt  string
	Tabs    int // question tabs in the strip above the prompt, 0 without one
	Options []ChoiceOption
	// Multi is true when the options carry check boxes.
	Multi bool
	// Review is true on a Review page: Options holds Submit only.
	Review bool
	// Cursor is the index in Options of the row under the cursor, -1 when
	// no row carries one.
	Cursor int
	// TextRow is the index of the "type your own" row, -1 when absent.
	TextRow int

	// How an agent's dialog is driven when it differs from the arrows and
	// Enter (agentchoice.go):
	//
	// Digits: the digit n picks row n (toggles it on a multi-select, opens
	// the field of the text row), whatever the cursor; Kimi draws no
	// cursor on its multi-select.
	Digits bool
	// InPlace: the text row takes typing as soon as the cursor is on it,
	// and Enter submits what was typed (Crush).
	InPlace bool
	// Toggle: the options are two buttons whose highlight the capture
	// cannot show; Cursor is the agent's default, Left flips it and Enter
	// takes it (Crush's yes/no, whose y/n keys record No inside a batch).
	Toggle bool
	// Next is the key that ends a multi-select and moves on; "" is Enter.
	Next string
	// Typing: the text row's field is open and takes the words.
	Typing bool
}

// ChoiceOption is one row of the list.
type ChoiceOption struct {
	Label   string
	Detail  string
	Checked bool
	// Text marks the row that opens a text field instead of answering.
	Text bool
}

var (
	// choiceRow is an option row: an optional cursor, a radio or box
	// mark, and the label.
	choiceRow = regexp.MustCompile(`^(\s*)(?:([` + cursorRunes + `])\s+)?([○●◉◯☐☑☒]|\[[ xX✔✓]\])\s+(\S.*)$`)
	// cursorOnly is a row with a cursor and no mark: Submit on a Review page.
	cursorOnly = regexp.MustCompile(`^(\s*)([` + cursorRunes + `])\s+(\S.*)$`)
	// textRow is the row that takes typed words instead of a pick.
	textRow = regexp.MustCompile(`(?i)\b(type (your own|something|a response|an answer|your answer)|custom (answer|input|response)|write your own|enter (your own|text|a value))\b`)
	// recommended is the suffix omp adds to the option its model suggests.
	recommended = regexp.MustCompile(`(?i)\s*\((recommended|default|suggested)\)\s*$`)
	// boxTop is a box's top edge carrying a title: "╭─ Ask ─────╮".
	boxTop = regexp.MustCompile(`^\s*[╭┌┏╔]─+\s*(\S.*?)\s*─+[╮┐┓╗]?\s*$`)
	// reviewAnswer is one answered question on a Review page: "1. Areas: Backend".
	reviewAnswer = regexp.MustCompile(`^\s*\d{1,2}\.\s+\S`)

	hintEnter    = regexp.MustCompile(`[⏎↵]|(?i)\benter\b`)
	hintAction   = regexp.MustCompile(`(?i)\b(select|toggle|next|submit|choose|confirm|pick|continue)\b`)
	hintNav      = regexp.MustCompile(`[↑↓␣]|(?i)\b(navigate|move|toggle|scroll|arrow)\b`)
	hintEsc      = regexp.MustCompile(`⎋|(?i)\besc(ape)?\b`)
	hintComposer = regexp.MustCompile(`(?i)\b(send|newline|shortcuts|quit|transcript|clear)\b|\?\s+for\b`)
)

// choiceFooter reports a key-hint line that drives a selection: Enter
// with a verb that picks, and a way to move or cancel; never an input
// box's own footer ("⏎ send · ⌃C quit").
func choiceFooter(l string) bool {
	return hintEnter.MatchString(l) && hintAction.MatchString(l) && (hintNav.MatchString(l) || hintEsc.MatchString(l)) &&
		!hintComposer.MatchString(l)
}

// ParseChoice reads a selection dialog at the bottom of screen.
func ParseChoice(screen string) (Choice, bool) {
	if c, ok := parseAgentChoice(screen); ok {
		return c, true
	}
	raw := window(screen)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = unbox(l)
	}
	foot := -1
	for i := len(lines) - 1; i >= 0 && footerLines(lines, i) <= maxFooter; i-- {
		if choiceFooter(lines[i]) {
			foot = i
			break
		}
	}
	if foot < 0 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1}
	// The options, bottom up: a row, or a description line indented past
	// the marks, which belongs to the row above it.
	i := foot - 1
	for i >= 0 && isFiller(lines[i]) {
		i--
	}
	type row struct {
		opt    ChoiceOption
		cursor bool
		dot    bool // a filled radio, the selection where no cursor is drawn
	}
	var rows []row // bottom up
	var desc []string
	col, top := -1, i
	family := ""
	for ; i >= 0; i-- {
		l := lines[i]
		if isFiller(l) {
			break
		}
		if m := choiceRow.FindStringSubmatchIndex(l); m != nil {
			at := utf8.RuneCountInString(l[:m[6]])
			mark := l[m[6]:m[7]]
			fam := "radio"
			if r, _ := utf8.DecodeRuneInString(mark); strings.ContainsRune("☐☑☒[", r) {
				fam = "box"
			}
			if col >= 0 && (at != col || fam != family) {
				break
			}
			col, family = at, fam
			label := strings.TrimSpace(l[m[8]:m[9]])
			r := row{cursor: m[4] >= 0, dot: mark == "●" || mark == "◉"}
			r.opt = ChoiceOption{Label: label, Detail: strings.Join(desc, " "),
				Checked: fam == "box" && mark != "☐" && mark != "[ ]" || fam == "radio" && r.dot, Text: textRow.MatchString(label)}
			desc = nil
			rows = append(rows, r)
			top = i
			continue
		}
		if m := cursorOnly.FindStringSubmatch(l); m != nil && len(rows) == 0 && len(desc) == 0 {
			// A cursor with no mark: Submit on a Review page, alone.
			label := strings.TrimSpace(m[3])
			if strings.EqualFold(label, "submit") {
				rows = append(rows, row{opt: ChoiceOption{Label: label}, cursor: true})
				top = i
				continue
			}
			break
		}
		indent := utf8.RuneCountInString(l) - utf8.RuneCountInString(strings.TrimLeft(l, " "))
		if (col < 0 || indent > col+1) && len(rows) == 0 || indent > col+1 && col >= 0 {
			desc = append([]string{strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "↳"))}, desc...)
			continue
		}
		break
	}
	if len(rows) == 0 {
		return Choice{}, false
	}
	for j := len(rows) - 1; j >= 0; j-- {
		r := rows[j]
		idx := len(c.Options)
		if r.cursor {
			c.Cursor = idx
		}
		if r.opt.Text && c.TextRow < 0 {
			c.TextRow = idx
		}
		r.opt.Label = recommended.ReplaceAllString(r.opt.Label, "")
		c.Options = append(c.Options, r.opt)
	}
	c.Multi = family == "box"
	if c.Cursor < 0 && family == "radio" {
		n, at := 0, -1
		for j, r := range rows {
			if r.dot {
				n, at = n+1, len(rows)-1-j
			}
		}
		if n == 1 {
			c.Cursor = at
		}
	}
	// The prompt: the first line of words above the list, past the rule,
	// with a tab strip above or below it when the questions come in turn.
	p := top - 1
	if len(desc) > 0 {
		p = top - 1 - len(desc)
	}
	for ; p >= 0; p-- {
		l := lines[p]
		if isFiller(l) {
			continue
		}
		t := strings.TrimSpace(l)
		if n := tabCount(t); n > 0 && c.Tabs == 0 && c.Prompt == "" {
			c.Tabs = n
			continue
		}
		if m := boxTop.FindStringSubmatch(raw[p]); m != nil {
			if c.Prompt == "" {
				c.Prompt = strings.TrimSpace(m[1])
			}
			break
		}
		if c.Prompt != "" {
			if n := tabCount(t); n > 0 {
				c.Tabs = n
			}
			break
		}
		if echoLine(t) || IsKeyHint(t) {
			break
		}
		c.Prompt = t
	}
	if len(c.Options) == 1 && strings.EqualFold(c.Options[0].Label, "submit") {
		c.Review = c.Prompt == "" || strings.Contains(strings.ToLower(c.Prompt), "review") || reviewAnswer.MatchString(c.Prompt)
		if !c.Review {
			return Choice{}, false
		}
		if c.Prompt == "" || reviewAnswer.MatchString(c.Prompt) {
			c.Prompt = "Review answers"
		}
		return c, true
	}
	if c.Prompt == "" || len(c.Options) < 2 && c.TextRow < 0 {
		return Choice{}, false
	}
	return c, true
}

// tabCount counts the tabs of a strip: words set apart by runs of spaces,
// the last of them Submit ("Areas    Rollout    Submit"). 0 otherwise.
func tabCount(t string) int {
	parts := strings.Split(t, "   ")
	var tabs []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			tabs = append(tabs, p)
		}
	}
	if len(tabs) < 2 || !strings.EqualFold(tabs[len(tabs)-1], "submit") {
		return 0
	}
	return len(tabs) - 1
}

// OptionIndex returns the index of the option labelled label.
func (c Choice) OptionIndex(label string) (int, bool) {
	for i, o := range c.Options {
		if !o.Text && strings.EqualFold(normalise(o.Label), normalise(label)) {
			return i, true
		}
	}
	return 0, false
}

// matchChoice turns a selection dialog into a prompt: its options without
// the text row, each picked by moving the cursor to it and pressing Enter.
// A list with check boxes, a tab strip, or no cursor cannot be answered by
// one key and is ReadOnly: an adapter's question item answers it.
func matchChoice(screen string) (Prompt, bool) {
	c, ok := ParseChoice(screen)
	if !ok || c.Prompt == "" {
		return Prompt{}, false
	}
	if c.Review {
		// Between the questions and the agent: the phone is told, with
		// nothing to press.
		return Prompt{Kind: KindWaiting, Title: c.Prompt, Keys: map[int][]string{}, ReadOnly: true}, true
	}
	if c.Cursor < 0 {
		// A list nothing points at is shown, not asked.
		return Prompt{}, false
	}
	// The keys move the cursor to a row and press Enter; on a list with
	// check boxes or a tab strip that is one step of several, so the
	// prompt is ReadOnly and its keys are only for a driver that knows the
	// dialog.
	p := Prompt{Kind: KindQuestion, Title: c.Prompt, Keys: map[int][]string{}, ReadOnly: c.Multi || c.Tabs > 0}
	for i, o := range c.Options {
		if o.Text {
			continue
		}
		n := len(p.Options) + 1
		p.Options = append(p.Options, wire.Option{N: n, Label: o.Label, Role: roleOf(o.Label)})
		p.Keys[n] = moveKeys(i - c.Cursor)
	}
	if len(p.Options) < 2 || len(p.Options) > maxOptions {
		return Prompt{}, false
	}
	return p, true
}

// moveKeys moves the cursor d rows (down when positive) and presses Enter.
func moveKeys(d int) []string {
	var keys []string
	for ; d > 0; d-- {
		keys = append(keys, "Down")
	}
	for ; d < 0; d++ {
		keys = append(keys, "Up")
	}
	return append(keys, "Enter")
}

// matchWaiting is the last resort: a boxed dialog whose footer says how
// to submit or cancel, with no list the other shapes could read (omp's
// custom-answer field, a Review page, a form). The agent waits at it and
// nothing typed would reach its input, so the phone is told, with the
// screen as the excerpt and nothing to press.
func matchWaiting(raw []string) (Prompt, bool) {
	foot := -1
	for i := len(raw) - 1; i >= 0 && footerLines(raw, i) <= 2; i-- {
		u := unbox(raw[i])
		if u != raw[i] && choiceFooter(u) {
			foot = i
			break
		}
	}
	if foot < 0 {
		return Prompt{}, false
	}
	title := ""
	for i := foot - 1; i >= 0 && i >= foot-12; i-- {
		if m := boxTop.FindStringSubmatch(raw[i]); m != nil {
			title = strings.TrimSpace(m[1])
			break
		}
		u := strings.TrimSpace(unbox(raw[i]))
		if u == "" || isBorder(u) || IsKeyHint(u) || len(u) < 3 || unbox(raw[i]) == raw[i] {
			if unbox(raw[i]) == raw[i] && !isBorder(u) && u != "" {
				break // out of the box without a title
			}
			continue
		}
		if title == "" {
			title = u
		}
	}
	if title == "" {
		title = "Waiting for input"
	}
	return Prompt{Kind: KindWaiting, Title: title, Keys: map[int][]string{}, ReadOnly: true}, true
}
