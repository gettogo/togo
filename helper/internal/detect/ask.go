package detect

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Ask is Claude Code's AskUserQuestion dialog as drawn on screen: an
// optional tab strip (one tab per question, then Submit), the current
// question's prompt and numbered options (each with an optional
// description line below it; multi-select options carry a [ ] or [✔] box),
// a "Type something" option, and below a rule a "Chat about this" option.
// After the last tab the dialog shows a review with "Submit answers".
type Ask struct {
	// Tabs are the tab strip's question tabs, without Submit; empty for a
	// dialog with one question and no strip.
	Tabs []AskTab
	// Review is true on the Submit tab: Prompt is empty and Options are
	// Submit answers and Cancel.
	Review bool
	Prompt string
	// Options are the current question's choices, without Type something
	// and Chat about this.
	Options []AskOption
	// Multi is true when the options carry check boxes.
	Multi bool
	// TypeN is the number of the "Type something" option, 0 when absent.
	TypeN int
	// SubmitN is the number of "Submit answers" on the review, 0 elsewhere.
	SubmitN int
	// Cursor is the number of the option the ❯ marker is on, 0 when none.
	Cursor int
}

// AskTab is one question tab: its header and whether it was answered.
type AskTab struct {
	Label    string
	Answered bool
}

// AskOption is one numbered choice.
type AskOption struct {
	N       int
	Label   string
	Checked bool
}

var (
	askOption = regexp.MustCompile(`^(\s*)(❯\s*)?(\d{1,2})\.\s+(?:\[([ ✔✓xX])\]\s+)?(.*?)\s*$`)
)

// ParseAsk reads an AskUserQuestion dialog at the bottom of screen.
func ParseAsk(screen string) (Ask, bool) {
	lines := window(screen)
	foot := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-4; i-- {
		low := strings.ToLower(lines[i])
		if strings.Contains(low, "enter to select") || strings.Contains(low, "ready to submit your answers") ||
			strings.Contains(lines[i], "Submit answers") || strings.Contains(lines[i], "Cancel") {
			foot = i
			break
		}
	}
	if foot < 0 {
		return Ask{}, false
	}
	var a Ask
	strip := -1
	for i := foot; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); strings.HasPrefix(t, "←") && strings.HasSuffix(t, "→") {
			strip = i
			a.Tabs = parseTabs(t)
			break
		}
	}
	start := strip + 1
	if strip < 0 {
		// No strip: the dialog starts after the last rule above its
		// options, or at the prompt line before them.
		start = 0
		for i := foot; i >= 0; i-- {
			if isBorder(lines[i]) && i+1 < len(lines) && !strings.Contains(lines[i+1], "Chat about this") {
				start = i + 1
				break
			}
		}
	}
	for _, l := range lines[start:] {
		if strings.Contains(l, "Ready to submit your answers") || strings.TrimSpace(l) == "Review your answers" {
			a.Review = true
		}
	}
	optIndent := -1
	submitRow := false
	promptAt := -1
	for i := start; i < len(lines); i++ {
		l := lines[i]
		if isBorder(l) {
			continue
		}
		m := askOption.FindStringSubmatch(l)
		if m == nil {
			t := strings.TrimSpace(l)
			if a.Prompt == "" && len(a.Options) == 0 && a.TypeN == 0 && !a.Review {
				if t != "" {
					a.Prompt, promptAt = t, i
				}
			} else if promptAt == i-1 && len(a.Options) == 0 && a.TypeN == 0 && askHeader(lines[promptAt], l) {
				// The line read as the prompt was the question's header,
				// drawn on its own line above it (Auggie 1.2.0's ask-user).
				a.Prompt, promptAt = t, i
			} else if len(a.Options) > 0 && (t == "Submit" || t == "Next") {
				// A multi-select's own Submit (or Next) row, unnumbered.
				submitRow = true
			}
			continue
		}
		indent := utf8.RuneCountInString(m[1] + m[2])
		if optIndent >= 0 && m[2] == "" && indent > optIndent+1 {
			continue // a description line that starts with a number
		}
		n, _ := strconv.Atoi(m[3])
		label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[5]), "✔"))
		if m[2] != "" {
			a.Cursor = n
		}
		if optIndent < 0 {
			optIndent = indent
		}
		switch {
		case strings.HasPrefix(strings.ToLower(label), "type something"):
			a.TypeN = n
		case label == "Chat about this":
		case a.Review && label == "Submit answers":
			a.SubmitN = n
		case a.Review:
		default:
			if m[4] != "" {
				a.Multi = true
			}
			a.Options = append(a.Options, AskOption{N: n, Label: label, Checked: m[4] != "" && m[4] != " "})
		}
	}
	if a.Review {
		a.Prompt = ""
		return a, a.SubmitN > 0
	}
	// A question dialog has a prompt, options, and Claude's own "Type
	// something" choice; a plain numbered menu has no Type something. A
	// multi-select without it ends in its own Submit row instead (Auggie).
	if a.Prompt == "" || len(a.Options) == 0 || a.TypeN == 0 && !(a.Multi && submitRow) {
		return Ask{}, false
	}
	return a, true
}

// askHeader reports a header line drawn above its question: short (a
// header is at most 16 characters), no question mark, and indented deeper
// than the question under it.
func askHeader(header, question string) bool {
	h := strings.TrimSpace(header)
	return h != "" && utf8.RuneCountInString(h) <= 16 && !strings.HasSuffix(h, "?") &&
		indentOf(header) > indentOf(question) && strings.TrimSpace(question) != ""
}

func parseTabs(strip string) []AskTab {
	var tabs []AskTab
	body := strings.Trim(strip, "←→ ")
	mark, label := rune(0), []rune{}
	flush := func() {
		l := strings.TrimSpace(string(label))
		done := mark == '✔' || mark == '✓'
		if mark != 0 && l != "" && !(done && l == "Submit") {
			tabs = append(tabs, AskTab{Label: l, Answered: mark == '☒' || done || mark == '■'})
		}
	}
	for _, r := range body {
		// Claude Code marks tabs ☐ / ☒ and Submit ✔, Auggie □ / ■ and ✓.
		if r == '☐' || r == '☒' || r == '✔' || r == '□' || r == '■' || r == '✓' {
			flush()
			mark, label = r, label[:0]
			continue
		}
		label = append(label, r)
	}
	flush()
	return tabs
}

// OptionN returns the number of the option labelled label.
func (a Ask) OptionN(label string) (int, bool) {
	for _, o := range a.Options {
		if normalise(o.Label) == normalise(label) {
			return o.N, true
		}
	}
	return 0, false
}

// askPrompt turns a question dialog into a Prompt for the approvals store:
// the current question's options without their descriptions. A dialog
// with tabs or check boxes cannot be answered by one key, so it is marked
// ReadOnly; the question item answers it.
func askPrompt(a Ask) Prompt {
	p := Prompt{Kind: KindQuestion, Title: a.Prompt, Keys: map[int][]string{}, ReadOnly: len(a.Tabs) > 0 || a.Multi}
	for _, o := range a.Options {
		p.Options = append(p.Options, wire.Option{N: o.N, Label: o.Label, Role: wire.RoleOther})
		p.Keys[o.N] = []string{strconv.Itoa(o.N)}
	}
	return p
}
