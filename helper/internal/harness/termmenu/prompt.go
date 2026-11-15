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
	Help  string
	Empty bool
}

var (
	promptTop    = regexp.MustCompile(`^[╭┌]─+\s*([^─].*?)\s*─+[╮┐]$`)
	promptBottom = regexp.MustCompile(`^[╰└]─+[╯┘]$`)
	promptInput  = regexp.MustCompile(`^[>❯]\s?(.*)$`)
)

// ReadPrompt reads a text prompt from a screen: a titled box holding exactly
// one input line and a line of key hints, and no rows to choose from. A box
// with anything else in it is not read as one.
func ReadPrompt(screen string) (Prompt, bool) {
	lines := parse(screen)
	for top := len(lines) - 1; top >= 0; top-- {
		g := promptTop.FindStringSubmatch(strings.TrimSpace(text(lines[top])))
		if g == nil {
			continue
		}
		p := Prompt{Title: strings.TrimSpace(g[1])}
		inputs, closed := 0, false
		var help []string
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
				p.Empty = strings.TrimSpace(promptInput.FindStringSubmatch(t)[1]) == ""
			case hintWords.MatchString(t):
				p.Hint = t
			default:
				help = append(help, t)
			}
		}
		if !closed || inputs != 1 || p.Hint == "" || p.Title == "" || len(help) > 3 {
			return Prompt{}, false
		}
		p.Help = strings.Join(help, " ")
		return p, true
	}
	return Prompt{}, false
}
