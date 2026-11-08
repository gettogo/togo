package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Claude 2.1.319 draws its empty input row as "❯" + U+00A0 and echoes sent
// prompts in the scrollback as "❯ text". The empty row must win, or every
// later send finds "uncleared text" and types nothing.
func TestInputLineToleratesUnicodeSpaces(t *testing.T) {
	a, err := compileAdapter(harness.AdapterFiles()["claude"], "2.1.319")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ screen, want string }{
		"nbsp empty":   {"❯ Reply with only the word ok.\n\n⏺ ok\n\n────\n❯ \n────\n  ? for shortcuts", ""},
		"nbsp text":    {"❯ old prompt\n────\n❯ new text\n────", "new text"},
		"narrow nbsp":  {"❯ old\n────\n❯  \n────", ""},
		"plain spaces": {"❯ old\n────\n❯ \n────", ""},
		"nbsp inside":  {"────\n❯ a b\n────", "a b"},
	} {
		got, ok := a.readInput(c.screen)
		if !ok || got != c.want {
			t.Errorf("%s: input = %q, %v; want %q", name, got, ok, c.want)
		}
	}
}
