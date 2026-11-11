package detect

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Question dialogs two agents draw without the marks ParseChoice reads,
// read from real captures (adapters/{crush,kimi}/testdata/screens).
//
// Crush 0.100 draws its question tool inline under the chat: "?  <prompt>",
// a description paragraph, then each option as a block of lines (label,
// then its description) set apart by blank lines; a "┃" bar left of the
// block under the cursor; "○/◉" on a multi choice; a last "> Something
// else?" row that takes typing as soon as the cursor is on it; a yes/no as
// a row of two buttons whose highlight only colour shows; free text as a
// "┃ Type your answer..." editor; a batch adds a strip of boxed tabs and
// ends on a "Ready to go?"-style confirm page with Yup! / Not yet. A wide
// pane puts a sidebar to the right of every line.
//
// Kimi Code 2.5 draws a "question" panel: a tab strip ending in Submit,
// "? <prompt>", numbered rows "→ [1] Red" with the description on the next
// line indented deeper, "[ ] / [✓]" rows on a multi-select (its cursor is
// colour only), an "[n] Other" row that opens a field in place, and a
// review page "Ready to submit your answers?" before anything is sent.

// Kimi.
var (
	kimiRow    = regexp.MustCompile(`^(\s*)(?:(→)\s+)?\[(\d{1,2})\]\s+(\S.*)$`)
	kimiBox    = regexp.MustCompile(`^(\s*)(?:(→)\s+)?\[([ ✓xX])\]\s+(\S.*)$`)
	kimiFooter = regexp.MustCompile(`\besc cancel\b`)
	kimiVerb   = regexp.MustCompile(`\b(choose|toggle|save|confirm)\b`)
	kimiTabTag = regexp.MustCompile(`\([✓○]\)\s*`)
	spaceRun   = regexp.MustCompile(`\s{2,}`)
)

// Crush.
var (
	crushPrompt = regexp.MustCompile(`^\s{0,3}\?\s+(\S.*)$`)
	crushFooter = regexp.MustCompile(`^\s*\[ prev tab • \] next tab\b|quick select|space toggle|enter done|shift\+enter newline|y yes • n no`)
	crushTabs   = regexp.MustCompile(`│\s*([^│]+?)\s*│`)
	crushMulti  = regexp.MustCompile(`^([○◉●])\s+(\S.*)$`)
	crushButton = regexp.MustCompile(`^\s{2,}(\S+(?: \S+)?)\s{3,}(\S+(?: \S+)?)$`)
)

// parseAgentChoice reads Crush's or Kimi's question dialog.
func parseAgentChoice(screen string) (Choice, bool) {
	if c, ok := parseHarnessChoice(screen); ok {
		return c, true
	}
	lines := plainLines(screen)
	if c, ok := parseKimiChoice(lines); ok {
		return c, true
	}
	return parseCrushChoice(cutSidebar(lines))
}

// plainLines is the whole screen without escapes, blank lines kept: the
// gaps between Crush's option blocks are what separates them.
func plainLines(screen string) []string {
	if len(screen) > maxScreenBytes {
		screen = screen[len(screen)-maxScreenBytes:]
	}
	screen = ansiSeq.ReplaceAllString(screen, "")
	all := strings.Split(screen, "\n")
	for i, l := range all {
		all[i] = strings.TrimRight(stripControls(l), " \t")
	}
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	return all
}

// lastNonBlank is the index of the bottom line with content, -1 for none.
func lastNonBlank(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func parseKimiChoice(lines []string) (Choice, bool) {
	foot := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-6; i-- {
		t := strings.TrimSpace(lines[i])
		if kimiFooter.MatchString(t) && kimiVerb.MatchString(t) {
			foot = i
			break
		}
	}
	if foot < 0 {
		return Choice{}, false
	}
	// The panel: from its "question" header down to the footer.
	head := -1
	for i := foot - 1; i >= 0 && i >= foot-40; i-- {
		if strings.TrimSpace(lines[i]) == "question" {
			head = i
			break
		}
	}
	if head < 0 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1, Digits: true, Next: "Tab"}
	body := lines[head+1 : foot]
	ft := strings.TrimSpace(lines[foot])
	c.Typing = strings.HasPrefix(ft, "type answer")
	review := false
	for _, l := range body {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasSuffix(t, "Submit") && c.Tabs == 0 && c.Prompt == "":
			tabs := spaceRun.Split(strings.TrimSpace(kimiTabTag.ReplaceAllString(t, "")), -1)
			if len(tabs) >= 2 {
				c.Tabs = len(tabs) - 1
			}
		case strings.HasPrefix(t, "Review your answer"):
			review = true
		case review && strings.HasSuffix(t, "?") && !strings.HasPrefix(t, "Q "):
			c.Prompt = t
		case strings.HasPrefix(t, "? ") && c.Prompt == "":
			c.Prompt = strings.TrimSpace(t[2:])
		}
	}
	if c.Prompt == "" {
		return Choice{}, false
	}
	labelCol := -1
	for _, l := range body {
		m := kimiRow.FindStringSubmatchIndex(l)
		box := false
		if m == nil {
			if m = kimiBox.FindStringSubmatchIndex(l); m == nil {
				// A description line under the row above, indented past
				// its label.
				if n := len(c.Options); n > 0 && labelCol >= 0 && indentOf(l) >= labelCol && strings.TrimSpace(l) != "" {
					o := &c.Options[n-1]
					o.Detail = strings.TrimSpace(o.Detail + " " + strings.TrimSpace(l))
				}
				continue
			}
			box = true
		}
		label := strings.TrimSpace(l[m[8]:m[9]])
		labelCol = utf8.RuneCountInString(l[:m[8]])
		if box {
			c.Multi = true
		} else if n, _ := strconv.Atoi(l[m[6]:m[7]]); n != len(c.Options)+1 {
			continue
		}
		o := ChoiceOption{Label: label, Checked: box && l[m[6]:m[7]] != " "}
		if lead, _, ok := strings.Cut(label, ":"); strings.EqualFold(label, "other") || ok && strings.EqualFold(lead, "other") {
			o.Label, o.Text = "Other", true
		}
		if m[4] >= 0 {
			c.Cursor = len(c.Options)
		}
		if o.Text && c.TextRow < 0 {
			c.TextRow = len(c.Options)
		}
		c.Options = append(c.Options, o)
	}
	if review {
		c.Review = true
		c.Options, c.Cursor, c.TextRow = []ChoiceOption{{Label: "Submit"}}, 0, -1
		return c, true
	}
	if len(c.Options) < 2 && c.TextRow < 0 {
		return Choice{}, false
	}
	return c, true
}

// cutSidebar drops a right-hand column that sits at the same place on
// many lines after a gap: Crush's sidebar on a wide pane.
func cutSidebar(lines []string) []string {
	count := map[int]int{}
	for _, l := range lines {
		r := []rune(l)
		for p := 40; p < len(r); p++ {
			if r[p] != ' ' && r[p-1] == ' ' && r[p-2] == ' ' {
				count[p]++
				break
			}
		}
	}
	col, best := -1, 0
	for p, n := range count {
		if n > best || n == best && p < col {
			col, best = p, n
		}
	}
	if best < 6 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		r := []rune(l)
		if len(r) > col {
			r = r[:col]
		}
		out[i] = strings.TrimRight(string(r), " ")
	}
	return out
}

func parseCrushChoice(lines []string) (Choice, bool) {
	foot := lastNonBlank(lines)
	if foot < 0 || !crushFooter.MatchString(lines[foot]) {
		return Choice{}, false
	}
	p := -1
	for i := foot - 1; i >= 0 && i >= foot-40; i-- {
		if crushPrompt.MatchString(lines[i]) {
			p = i
			break
		}
	}
	if p < 0 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1, Prompt: strings.TrimSpace(crushPrompt.FindStringSubmatch(lines[p])[1])}
	for i := p - 1; i >= 0 && i >= p-4; i-- {
		if strings.Contains(lines[i], "││") {
			if n := len(crushTabs.FindAllString(strings.ReplaceAll(lines[i], "││", "│ │"), -1)); n > 1 {
				c.Tabs = n - 1 // the last tab is Confirm
			}
			break
		}
	}
	// Blocks of lines between blank lines, below the description.
	type block struct {
		lines  []string
		indent int
		bar    bool
	}
	var blocks []block
	var cur *block
	for i := p + 1; i < foot; i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			cur = nil
			continue
		}
		bar := false
		body := l
		if t := strings.TrimLeft(l, " "); strings.HasPrefix(t, "┃") {
			bar = true
			body = strings.Repeat(" ", indentOf(l)) + " " + strings.TrimPrefix(t, "┃")
		}
		if cur == nil {
			blocks = append(blocks, block{indent: indentOf(body), bar: bar})
			cur = &blocks[len(blocks)-1]
		}
		cur.lines = append(cur.lines, strings.TrimSpace(body))
		cur.bar = cur.bar || bar
	}
	if len(blocks) == 0 {
		return Choice{}, false
	}
	ft := lines[foot]
	// The confirm page of a batch: a summary and two buttons.
	if strings.Contains(ft, "y yes • n no") && strings.Contains(ft, "scroll") {
		c.Review = true
		c.Options, c.Cursor = []ChoiceOption{{Label: "Submit"}}, 0
		return c, true
	}
	// A yes/no: the buttons' highlight is colour only, and Crush starts on
	// No, the safe answer.
	if strings.Contains(ft, "y yes • n no") {
		last := blocks[len(blocks)-1]
		m := crushButton.FindStringSubmatch(" " + strings.Repeat(" ", last.indent) + last.lines[0])
		if len(last.lines) != 1 || m == nil {
			return Choice{}, false
		}
		c.Options = []ChoiceOption{{Label: m[1]}, {Label: m[2]}}
		c.Cursor, c.Toggle = 1, true
		return c, true
	}
	// Free text: the editor alone, typed into at once.
	if strings.Contains(ft, "shift+enter newline") {
		c.Options = []ChoiceOption{{Label: "Type your answer", Text: true}}
		c.Cursor, c.TextRow, c.InPlace = 0, 0, true
		return c, true
	}
	// Options: the blocks at the option column (deeper than the
	// description paragraph).
	col := -1
	for _, b := range blocks {
		if b.indent > col {
			col = b.indent
		}
	}
	for _, b := range blocks {
		if b.indent != col {
			if len(c.Options) > 0 {
				break
			}
			continue // the description paragraph
		}
		label := b.lines[0]
		o := ChoiceOption{Label: label, Detail: strings.Join(b.lines[1:], " ")}
		if m := crushMulti.FindStringSubmatch(label); m != nil {
			c.Multi = true
			o.Label, o.Checked = m[2], m[1] != "○"
		}
		if strings.HasPrefix(o.Label, "> ") {
			o.Label, o.Text = "Something else?", true
			c.TextRow, c.InPlace = len(c.Options), true
		}
		if b.bar {
			c.Cursor = len(c.Options)
		}
		c.Options = append(c.Options, o)
	}
	if len(c.Options) < 2 || c.Cursor < 0 {
		return Choice{}, false
	}
	return c, true
}

// matchAgentChoice turns Crush's or Kimi's dialog into a prompt: Kimi's
// rows picked by their digit, Crush's by moving the bar, a yes/no from its
// default; a review page, a text field or a batch is shown, not answered
// with one key.
func matchAgentChoice(screen string) (Prompt, bool) {
	c, ok := parseAgentChoice(screen)
	if !ok {
		return Prompt{}, false
	}
	if c.Review || c.Typing || len(c.Options) == 1 && c.TextRow == 0 {
		return Prompt{Kind: KindWaiting, Title: c.Prompt, Keys: map[int][]string{}, ReadOnly: true}, true
	}
	p := Prompt{Kind: KindQuestion, Title: c.Prompt, Keys: map[int][]string{}}
	// Kimi's single question still ends on its review page, which Enter
	// confirms; a batch needs every question answered first.
	p.ReadOnly = c.Multi || c.Digits && c.Tabs > 1 || !c.Digits && c.Tabs > 0
	for i, o := range c.Options {
		if o.Text {
			continue
		}
		n := len(p.Options) + 1
		p.Options = append(p.Options, wire.Option{N: n, Label: o.Label, Role: roleOf(o.Label)})
		switch {
		case c.Digits:
			p.Keys[n] = []string{strconv.Itoa(i + 1), "Enter"}
		case c.Toggle && i != c.Cursor:
			p.Keys[n] = []string{"Left", "Enter"}
		case c.Toggle:
			p.Keys[n] = []string{"Enter"}
		default:
			p.Keys[n] = moveKeys(i - c.Cursor)
		}
	}
	if len(p.Options) < 2 || len(p.Options) > maxOptions {
		return Prompt{}, false
	}
	return p, true
}
