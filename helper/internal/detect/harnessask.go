package detect

import (
	"regexp"
	"strconv"
	"strings"
)

// Question dialogs of Vibe, Copilot CLI, Cline and Droid, read from real
// captures (adapters/{vibe,copilot,cline,droid}/testdata/screens), for
// ParseChoice and so for the screen_answer driver:
//
// Vibe 3.1 draws a square box: a tab strip ("[Areas]   Rollout ✓") on a
// batch, the prompt, numbered rows "1. Label - description" (or
// "1. [ ] Label" on a multi-select), "N. Type your answer...", a
// "Next →"/"Submit →" line on a multi-select, footer "↑↓/jk navigate  Enter
// select  Esc cancel". The cursor is reverse video only; a digit picks its
// row. Typing in the answer row turns the footer's hint to "Ctrl+J
// newline". Its tool permission is the same box: "Permission for the …
// tool", the command, rows "1. Allow once" … "4. Deny".
//
// Copilot CLI 1.0 draws its ask_user form between two rules: "Copilot
// needs information.", the message, a tab strip "[Platforms]  Caching" on
// a form of several fields, the field's label ("Caching:"), rows under a
// "❯" cursor ("✓"/"•" before each on a multi-select), "Other (type your
// answer)", footer "↑/↓ select · enter accept · …". Choosing Other opens an
// input between two more rules under the footer.
//
// Cline 3.0 draws its ask_question unboxed and without a footer: "Cline
// is asking a question", the prompt, rows under a "> " cursor, and a last
// "Type a response..." row that takes typing in place (the typed words
// replace its label, with a "|" caret).
//
// Droid 1.5 draws AskUser unboxed: the question, numbered rows, "Or type
// your own answer...", footer "↑/↓ Navigate • Space/Enter/1–9 Select";
// ExitSpecMode's approval is a box of numbered rows under "Propose
// Specification "<title>"" with footer "↑↓ navigate   1-5 select   Enter
// select   Tab reasoning", and its autonomy picker the same box with "Esc
// back".

var (
	numberedRow  = regexp.MustCompile(`^(\s*)(?:([❯›>→])\s*)?(\d{1,2})\.\s+(?:\[([ xX✓])\]\s+)?(\S.*)$`)
	vibeFooter   = regexp.MustCompile(`↑↓/jk navigate`)
	vibeNextRow  = regexp.MustCompile(`^\s*(Next|Submit) →\s*$`)
	copilotHead  = "Copilot needs information."
	copilotFoot  = regexp.MustCompile(`(?i)\benter accept\b`)
	copilotRow   = regexp.MustCompile(`^ ([❯✓ ]) (?:([✓•]) )?(\S.*)$`)
	copilotTabs  = regexp.MustCompile(`\[[^\]]+\]`)
	clineHead    = "Cline is asking a question"
	clineRow     = regexp.MustCompile(`^  ([> ]) (\S.*)$`)
	droidAskFoot = regexp.MustCompile(`Space/Enter/1[–-]9 Select`)
	droidBoxFoot = regexp.MustCompile(`↑↓ navigate\s+1-\d select\s+Enter select`)
	droidOwnRow  = regexp.MustCompile(`(?i)^\s*or type your own answer`)
	droidSavings = regexp.MustCompile(`\s*\(\d+% context savings\)\s*$`)
	vibeDetail   = regexp.MustCompile(`^(.+?) - (.+)$`)
)

// droidAutonomyPrompt titles Droid's second spec picker, which has none.
const droidAutonomyPrompt = "Proceed with which autonomy?"

// parseHarnessChoice reads one of the dialogs above.
func parseHarnessChoice(screen string) (Choice, bool) {
	raw := window(screen)
	if len(raw) == 0 {
		return Choice{}, false
	}
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = unbox(l)
	}
	for _, f := range []func(raw, lines []string) (Choice, bool){vibeChoice, copilotChoice, copilotPlanChoice, clineChoice, droidChoice} {
		if c, ok := f(raw, lines); ok {
			return c, true
		}
	}
	return Choice{}, false
}

// footerAt is the index of the bottom line (within the last n) that re
// matches, -1 for none.
func footerAt(lines []string, re *regexp.Regexp, n int) int {
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-n; i-- {
		if re.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

// numberedRows reads the run of rows "1." … "n." that ends at line end,
// bottom up; rows must count from 1. top is the first row's index.
func numberedRows(lines []string, end int) (rows []ChoiceOption, cursor, top int) {
	cursor, top = -1, -1
	var marks []string
	var picked []bool
	// A long row wraps: the lines under it, up to the next row, carry on
	// its label.
	var cont []string
	for i := end; i >= 0; i-- {
		m := numberedRow.FindStringSubmatch(lines[i])
		if m == nil {
			t := strings.TrimSpace(lines[i])
			if t == "" || isBorder(t) || i == end || i < end-12 {
				break
			}
			cont = append([]string{t}, cont...)
			continue
		}
		n, _ := strconv.Atoi(m[3])
		if len(rows) > 0 && n != rows[0].numberHint()-1 {
			break
		}
		label := strings.TrimSpace(strings.Join(append([]string{m[5]}, cont...), " "))
		cont = nil
		o := ChoiceOption{Label: label, Detail: strconv.Itoa(n)}
		rows = append([]ChoiceOption{o}, rows...)
		marks = append([]string{m[4]}, marks...)
		picked = append([]bool{m[2] != ""}, picked...)
		top = i
		if n == 1 {
			break
		}
	}
	if len(rows) == 0 || rows[0].numberHint() != 1 || len(cont) > 0 && top < 0 {
		return nil, -1, -1
	}
	for i := range rows {
		rows[i].Detail = ""
		if marks[i] != "" {
			rows[i].Checked = marks[i] != " "
			rows[i].Detail = "box"
		}
		if picked[i] {
			cursor = i
		}
	}
	return rows, cursor, top
}

// numberHint is the row number numberedRows keeps in Detail while it
// reads.
func (o ChoiceOption) numberHint() int {
	n, _ := strconv.Atoi(o.Detail)
	return n
}

// boxTopAbove is the index of the top edge of the square or round box
// holding line i, -1 when there is none within 30 lines.
func boxTopAbove(raw []string, i int) int {
	for j := i; j >= 0 && j >= i-30; j-- {
		t := strings.TrimSpace(raw[j])
		if strings.HasPrefix(t, "┌") || strings.HasPrefix(t, "╭") {
			return j
		}
	}
	return -1
}

// tabStrip counts the tabs of a strip that brackets the current one:
// "[Areas]   Rollout ✓", "  Platforms  [Caching]  Project name".
func tabStrip(t string) int {
	if !strings.Contains(t, "[") || !strings.Contains(t, "]") {
		return 0
	}
	parts := spaceRun.Split(strings.TrimSpace(t), -1)
	if len(parts) < 2 || !copilotTabs.MatchString(t) {
		return 0
	}
	return len(parts)
}

func vibeChoice(raw, lines []string) (Choice, bool) {
	foot := footerAt(lines, vibeFooter, 3)
	if foot < 0 {
		return Choice{}, false
	}
	end := foot - 1
	for end >= 0 && isFiller(lines[end]) {
		end--
	}
	next := ""
	if end >= 0 && vibeNextRow.MatchString(lines[end]) {
		next = strings.Fields(lines[end])[0]
		end--
		for end >= 0 && isFiller(lines[end]) {
			end--
		}
	}
	rows, _, top := numberedRows(lines, end)
	if len(rows) == 0 {
		return Choice{}, false
	}
	boxTop := boxTopAbove(raw, top)
	if boxTop < 0 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1, Digits: true}
	c.Typing = strings.Contains(strings.ToLower(lines[foot]), "newline")
	for i := boxTop + 1; i < top; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || isBorder(t) {
			continue
		}
		if n := tabStrip(t); n > 0 && c.Prompt == "" {
			c.Tabs = n
			continue
		}
		if c.Prompt == "" {
			c.Prompt = t
		}
	}
	if c.Prompt == "" {
		return Choice{}, false
	}
	for i, o := range rows {
		box := o.Detail == "box"
		o.Detail = ""
		switch {
		case textRow.MatchString(o.Label), c.Typing && i == len(rows)-1:
			o.Text = true
			if c.TextRow < 0 {
				c.TextRow = i
			}
		case !box:
			if m := vibeDetail.FindStringSubmatch(o.Label); m != nil {
				o.Label, o.Detail = m[1], m[2]
			}
		}
		if box {
			c.Multi = true
		}
		c.Options = append(c.Options, o)
	}
	if c.Multi && next != "" {
		// Space or Enter toggles the row under the cursor; the Next or
		// Submit line under the list moves on.
		c.Next = "Right"
	}
	if len(c.Options) < 2 && c.TextRow < 0 {
		return Choice{}, false
	}
	return c, true
}

func copilotChoice(raw, lines []string) (Choice, bool) {
	foot := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-10; i-- {
		if copilotFoot.MatchString(lines[i]) && strings.Contains(lines[i], "·") {
			foot = i
			break
		}
	}
	if foot < 0 {
		return Choice{}, false
	}
	head := -1
	for i := foot - 1; i >= 0 && i >= foot-30; i-- {
		if strings.TrimSpace(lines[i]) == copilotHead {
			head = i
			break
		}
	}
	if head < 0 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1}
	// An open "Other" field: a "❯" line between rules under the footer.
	for i := foot + 1; i < len(lines); i++ {
		if t := strings.TrimSpace(lines[i]); strings.HasPrefix(t, "❯") {
			c.Typing = true
		}
	}
	top := foot
	var rows []ChoiceOption
	for i := foot - 1; i > head; i-- {
		m := copilotRow.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		o := ChoiceOption{Label: strings.TrimSpace(m[3]), Checked: m[2] == "✓" || m[1] == "✓" && m[2] == ""}
		if m[2] != "" {
			c.Multi = true
		}
		if m[1] == "❯" {
			c.Cursor = len(rows) // reversed below
		}
		rows = append(rows, o)
		top = i
	}
	if len(rows) == 0 {
		return Choice{}, false
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	if c.Cursor >= 0 {
		c.Cursor = len(rows) - 1 - c.Cursor
	}
	for i := range rows {
		if textRow.MatchString(rows[i].Label) {
			rows[i].Text = true
			if c.TextRow < 0 {
				c.TextRow = i
			}
		}
	}
	c.Options = rows
	// The prompt: the field's label on a form of several fields, else
	// the message's first line.
	var field, message string
	for i := head + 1; i < top; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if n := tabStrip(t); n > 0 {
			c.Tabs = n
			continue
		}
		if message == "" {
			message = t
			continue
		}
		if c.Tabs > 0 && field == "" {
			field = strings.TrimSuffix(t, ":")
		}
	}
	c.Prompt = message
	if c.Tabs > 0 && field != "" {
		c.Prompt = field
	}
	if c.Prompt == "" || len(c.Options) < 2 && c.TextRow < 0 {
		return Choice{}, false
	}
	return c, true
}

func clineChoice(raw, lines []string) (Choice, bool) {
	head := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-20; i-- {
		if strings.TrimSpace(lines[i]) == clineHead {
			head = i
			break
		}
	}
	if head < 0 || head+2 >= len(lines) {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1, InPlace: true, Prompt: strings.TrimSpace(lines[head+1])}
	i := head + 2
	for ; i < len(lines) && clineRow.FindStringSubmatch(lines[i]) == nil; i++ {
		if i > head+6 {
			return Choice{}, false
		}
	}
	for ; i < len(lines); i++ {
		m := clineRow.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		if m[1] == ">" {
			c.Cursor = len(c.Options)
		}
		c.Options = append(c.Options, ChoiceOption{Label: strings.TrimSpace(m[2])})
	}
	n := len(c.Options)
	if n < 2 || c.Prompt == "" {
		return Choice{}, false
	}
	// The last row is always the response row; once typed in, it shows
	// the words and a caret instead of its label.
	last := &c.Options[n-1]
	last.Text, c.TextRow = true, n-1
	if !textRow.MatchString(last.Label) {
		c.Typing = true
		last.Label = strings.TrimSuffix(last.Label, "|")
	}
	return c, true
}

func droidChoice(raw, lines []string) (Choice, bool) {
	if foot := footerAt(lines, droidAskFoot, 3); foot >= 0 {
		end := foot - 1
		text := -1
		if end >= 0 && droidOwnRow.MatchString(lines[end]) {
			text = end
			end--
		}
		rows, _, top := numberedRows(lines, end)
		if len(rows) == 0 || top < 1 {
			return Choice{}, false
		}
		c := Choice{Cursor: -1, TextRow: -1, Digits: true, Prompt: strings.TrimSpace(lines[top-1])}
		for _, o := range rows {
			if o.Detail == "box" {
				c.Multi = true
			}
			o.Detail = ""
			c.Options = append(c.Options, o)
		}
		if text >= 0 {
			c.TextRow = len(c.Options)
			c.Options = append(c.Options, ChoiceOption{Label: strings.TrimSpace(lines[text]), Text: true})
		}
		return c, c.Prompt != "" && len(rows) >= 2
	}
	foot := footerAt(lines, droidBoxFoot, 3)
	if foot < 0 {
		return Choice{}, false
	}
	end := foot - 1
	for end >= 0 && isFiller(lines[end]) {
		end--
	}
	rows, _, top := numberedRows(lines, end)
	if len(rows) < 2 {
		return Choice{}, false
	}
	c := Choice{Cursor: -1, TextRow: -1, Digits: true}
	for _, o := range rows {
		o.Detail = ""
		o.Label = droidSavings.ReplaceAllString(o.Label, "")
		c.Options = append(c.Options, o)
	}
	if strings.HasPrefix(c.Options[0].Label, "Proceed with Manual") {
		c.Prompt = droidAutonomyPrompt
		return c, true
	}
	c.Prompt = "Propose Specification" // its title scrolled away on a narrow pane
	for i := top - 1; i >= 0 && i >= top-40; i-- {
		if t := strings.TrimSpace(lines[i]); strings.HasPrefix(t, "Propose Specification") {
			c.Prompt = t
			break
		}
	}
	return c, true
}
