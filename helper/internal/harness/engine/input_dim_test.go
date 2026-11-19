package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Captured from Claude Code 2.1.327 with tmux capture-pane -e. After a
// reply Claude draws a suggested next prompt in its input line, dim: Tab
// would take it, but nothing is typed. Read as typed text it could not be
// cleared, and every send was refused with "input line already holds text".
func TestADimSuggestionIsNotTypedText(t *testing.T) {
	a, err := compileAdapter(harness.AdapterFiles()["claude"], "test")
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join("testdata", "input", name))
		if err != nil {
			t.Fatal(err)
		}
		return a.readInput(string(b))
	}
	if got, ok := read("claude-suggested-prompt.ansi"); !ok || got != "" {
		t.Errorf("suggested prompt: input = %q, found %v; want empty and found", got, ok)
	}
	// One character typed over the suggestion: the line holds that character.
	if got, ok := read("claude-typed-over-suggestion.ansi"); !ok || got != "x" {
		t.Errorf("typed text: input = %q, found %v; want x", got, ok)
	}
}

func TestDimTextRules(t *testing.T) {
	a, err := compileAdapter(harness.AdapterFiles()["claude"], "test")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ line, want string }{
		"all dim":                  {"\x1b[39m❯ \x1b[2mrun the tests\x1b[0m", ""},
		"dim ended by 22":          {"❯ \x1b[2mrun the tests\x1b[22m", ""},
		"dim with a colour":        {"❯ \x1b[2;38;5;246mrun the tests\x1b[0m", ""},
		"typed, then dim rest":     {"❯ run\x1b[2m the tests\x1b[0m", "run the tests"},
		"typed, coloured, not dim": {"❯ \x1b[38;5;246mrun the tests\x1b[0m", "run the tests"},
		"dim turned off first":     {"\x1b[2m\x1b[0m❯ run the tests", "run the tests"},
		"no colour codes at all":   {"❯ run the tests", "run the tests"},
		"dim prompt mark only":     {"\x1b[2m❯\x1b[0m run the tests", "run the tests"},
	} {
		if got, ok := a.readInput("some output\n" + c.line + "\n"); !ok || got != c.want {
			t.Errorf("%s: input = %q, found %v; want %q", name, got, ok, c.want)
		}
	}
}
