package engine

import "testing"

// Captured from omp 18.20.2 at 46 columns: each text was typed into its
// input line and never submitted. The command is found only where every
// break is certain to be one space; where the screen cannot say (a break
// after the input's longest line, or inside a word) it is not found, and
// so is not run. A command changed at the desk is never taken for the one
// that was typed.
func TestACommandWiderThanTheTerminalIsFoundOnlyWhereItsBreaksAreCertain(t *testing.T) {
	const typed = "╰─ /add-dir"
	for _, c := range []struct {
		name, screen, whole string
		found               bool
	}{
		{"v1 expected", "╰─ /add-dir /tmp/togo-fixture-00000000/shared\n   docs", "╰─ /add-dir /tmp/togo-fixture-00000000/shared docs", false},
		{"v2 no sep after name", "╰─ /add-dir/tmp/togo-fixture-00000000/shared\n   docs", "╰─ /add-dir /tmp/togo-fixture-00000000/shared docs", false},
		{"v3 shareddocs", "╰─ /add-dir\n   /tmp/togo-fixture-00000000/shareddocs", "╰─ /add-dir /tmp/togo-fixture-00000000/shared docs", false},
		{"v4 three lines", "╰─ /add-dir /tmp/togo-fixture-00000000/a much\n   longer folder name with several words in it", "╰─ /add-dir /tmp/togo-fixture-00000000/a much longer folder name with several words in it", true},
		{"v5 long unbroken", "╰─ /add-dir /tmp/togo-fixture-00000000/xxxxxxx\n   xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n   xxxxxxxxxxxxxxxxxxxx", "╰─ /add-dir /tmp/togo-fixture-00000000/xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", false},
		{"v6 two spaces", "╰─ /add-dir  /tmp/togo-fixture-00000000/shared\n   docs", "╰─ /add-dir /tmp/togo-fixture-00000000/shared docs", false},
		{"v7 fill a", "╰─ /add-dir\n   aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n   tail", "╰─ /add-dir aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa tail", false},
		{"v8 fill a nospace", "╰─ /add-dir aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n   aaaaaaaaatail", "╰─ /add-dir aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa tail", false},
		{"v9 fill 42", "╰─ /add-dir\n   aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n   tail", "╰─ /add-dir aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa tail", false},
		{"v11 fill 41", "╰─ /add-dir\n   aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n   tail", "╰─ /add-dir aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa tail", false},
		{"v3 as typed", "╰─ /add-dir\n   /tmp/togo-fixture-00000000/shareddocs", "╰─ /add-dir /tmp/togo-fixture-00000000/shareddocs", true},
		{"v6 as typed", "╰─ /add-dir  /tmp/togo-fixture-00000000/shared\n   docs", "╰─ /add-dir  /tmp/togo-fixture-00000000/shared docs", false},
		{"one line", "╰─ /add-dir /tmp/a", "╰─ /add-dir /tmp/a", true},
		// Found in review: wide characters. The first line is 27
		// characters but 46 cells: it reaches the edge, and the space at
		// its break cannot be known. Counting characters took it for the
		// shorter line.
		{"wide characters", "╰─ /add-dir /tmp/資料夾資料夾資料夾資料夾資料夾資料夾資\n   shared docs and more words here ok", "╰─ /add-dir /tmp/資料夾資料夾資料夾資料夾資料夾資料夾資 shared docs and more words here ok", false},
		{"wide in a later line", "╰─ /add-dir\n   /tmp/資料夾/shared docs", "╰─ /add-dir /tmp/資料夾/shared docs", false},
		{"an emoji", "╰─ /add-dir\n   /tmp/🙂/shared docs here", "╰─ /add-dir /tmp/🙂/shared docs here", false},
		{"a combining mark", "╰─ /add-dir\n   /tmp/cafe\u0301/shared docs", "╰─ /add-dir /tmp/cafe\u0301/shared docs", false},
		{"wide on one line", "╰─ /add-dir /tmp/資料夾", "╰─ /add-dir /tmp/資料夾", true},
		{"only in a reply", "  /add-dir /tmp/a b\n╰─", "╰─ /add-dir /tmp/a b", false},
		{"a blank line apart", "╰─ /add-dir\n\n   /tmp/a b c d e f g h", "╰─ /add-dir /tmp/a b c d e f g h", false},
	} {
		found, rest := commandBlock(" status line\n"+c.screen+"\nbelow", typed, c.whole)
		if found != c.found {
			t.Errorf("%s: found %v, want %v", c.name, found, c.found)
		}
		if found && rest != " status line\nbelow" {
			t.Errorf("%s: rest %q", c.name, rest)
		}
	}
}
