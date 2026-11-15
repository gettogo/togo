package engine

import "testing"

// Seen live on a 46-column terminal (a phone's): omp wrapped
// "/add-dir <folder>" onto a second line, the command was not found on
// any one line, and it was refused with its text left in the input line.
func TestACommandWiderThanTheTerminalIsFoundAcrossItsLines(t *testing.T) {
	const typed = "╰─ /add-dir"
	const whole = "╰─ /add-dir /tmp/togo-fixture-00000000/shared docs"
	status := " π > ◒ Model > 🗑 …eck A ▶─3%──┃400K─"
	for name, c := range map[string]struct {
		screen string
		found  bool
		rest   string
	}{
		"one line":           {status + "\n" + whole, true, status},
		"wrapped at a word":  {status + "\n╰─ /add-dir\n   /tmp/togo-fixture-00000000/shared docs\n", true, status + "\n"},
		"wrapped in a word":  {status + "\n╰─ /add-dir /tmp/togo-fixture-000\n   00000/shared docs", true, status},
		"three lines":        {"╰─ /add-dir\n   /tmp/togo-fixture-00000000/shared\n   docs\nbelow", true, "below"},
		"other arguments":    {status + "\n╰─ /add-dir\n   /tmp/another folder", false, ""},
		"more text after":    {status + "\n╰─ /add-dir\n   /tmp/togo-fixture-00000000/shared docs and more", false, ""},
		"stops short":        {status + "\n╰─ /add-dir\n   /tmp/togo-fixture-00000000/shared", false, ""},
		"a space left out":   {"╰─ /add-dir\n   /tmp/togo-fixture-00000000/shareddocs", false, ""},
		"only in a reply":    {"  /add-dir /tmp/togo-fixture-00000000/shared docs\n╰─", false, ""},
		"a blank line apart": {"╰─ /add-dir\n\n   /tmp/togo-fixture-00000000/shared docs", false, ""},
	} {
		found, rest := commandBlock(c.screen, typed, whole)
		if found != c.found || (found && rest != c.rest) {
			t.Errorf("%s: found %v, rest %q", name, found, rest)
		}
	}
}
