package engine

import "strings"

// listLine is one line of a list drawn the way OpenCode draws its lists:
// a panel on its own background, group headings in another colour, the
// cursor's row on a third background.
type listLine struct {
	heading string
	label   string
	cursor  bool
}

func listScreen(title string, lines []listLine) string {
	const (
		panel = "\x1b[48;2;20;20;20m"
		hl    = "\x1b[48;2;250;178;131m"
		off   = "\x1b[48;2;4;4;4m"
		text  = "\x1b[38;2;238;238;238m"
		dim   = "\x1b[38;2;157;124;216m"
		width = 40
	)
	row := func(bg, fg, s string) string {
		return "  " + bg + fg + "   " + s + strings.Repeat(" ", width-len(s)) + "   " + off + "\n"
	}
	var b strings.Builder
	b.WriteString(row(panel, text, title+strings.Repeat(" ", width-len(title)-3)+"esc"))
	b.WriteString(row(panel, text, ""))
	for _, l := range lines {
		switch {
		case l.heading != "":
			b.WriteString(row(panel, dim, l.heading))
		case l.cursor:
			b.WriteString(row(hl, "\x1b[38;2;4;4;4m", l.label))
		default:
			b.WriteString(row(panel, text, l.label))
		}
	}
	b.WriteString(row(panel, text, ""))
	return b.String()
}
