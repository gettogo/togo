package termmenu

import (
	"regexp"
	"strings"
)

// Prompt is a panel that asks for one line of text: a title, an input line
// and the keys that submit or cancel it. Value is what the input line
// holds; it is read only to tell an empty field from one someone has typed
// into, and is never shown.
type Prompt struct {
	Title string
	Hint  string
	Empty bool
	// Focused: the harness draws its cursor in the input line. Row is the
	// screen row of that line, for a caller that knows where the
	// terminal's own cursor is.
	Focused bool
	Row     int
}

var (
	promptTop    = regexp.MustCompile(`^[╭┌]─+\s*([^─].*?)\s*─+[╮┐]$`)
	promptBottom = regexp.MustCompile(`^[╰└]─+[╯┘]$`)
	promptInput  = regexp.MustCompile(`^[>❯]\s?(.*)$`)
)

// ReadPrompt reads a text prompt from a screen: a titled box holding exactly
// one input line and a line of key hints, and no rows to choose from. A box
// with anything else in it is not read as one. Other text in the box is not
// kept: it may repeat what was entered.
func ReadPrompt(screen string) (Prompt, bool) {
	lines := parse(screen)
	for top := len(lines) - 1; top >= 0; top-- {
		g := promptTop.FindStringSubmatch(strings.TrimSpace(text(lines[top])))
		if g == nil {
			continue
		}
		p := Prompt{Title: strings.TrimSpace(g[1])}
		inputs, closed := 0, false
		help := 0
		for i := top + 1; i < len(lines) && i <= top+12; i++ {
			t := strings.TrimSpace(text(lines[i]))
			if promptBottom.MatchString(t) {
				closed = true
				break
			}
			t = strings.TrimSpace(strings.Trim(t, borders+rules))
			switch {
			case t == "":
			case promptInput.MatchString(t):
				inputs++
				p.Row = i
				p.Empty = strings.TrimSpace(promptInput.FindStringSubmatch(t)[1]) == ""
				// With a drawn cursor the field is empty only if the
				// cursor stands right after the mark: spaces typed at the
				// desk are text too.
				if at, ok := drawnIn(lines[i]); ok {
					p.Focused = true
					before := strings.TrimLeft(strings.TrimLeft(text(lines[i][:at]), " "), rules)
					before = strings.TrimPrefix(before, " ")
					p.Empty = p.Empty && (before == "> " || before == "❯ " || before == ">" || before == "❯")
				}
			case hintWords.MatchString(t):
				p.Hint = t
			default:
				help++
			}
		}
		if !closed || inputs != 1 || p.Hint == "" || p.Title == "" || help > 3 {
			return Prompt{}, false
		}
		return p, true
	}
	return Prompt{}, false
}

// drawnIn finds a single cell in reverse video in a line.
func drawnIn(line []cell) (int, bool) {
	at, n := -1, 0
	for i, c := range line {
		if c.rev && (i == 0 || !line[i-1].rev) && (i+1 >= len(line) || !line[i+1].rev) {
			at, n = i, n+1
		}
	}
	return at, n == 1
}
