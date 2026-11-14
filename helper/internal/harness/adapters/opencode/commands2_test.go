package opencode

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// slashRows reads a recording of the terminal's / menu: one line per name
// typed after the slash, "name => first row of the menu".
func slashRows(t *testing.T, name string) map[string]string {
	t.Helper()
	f, err := os.Open("testdata/v2/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string]string{}
	for sc := bufio.NewScanner(f); sc.Scan(); {
		if typed, row, ok := strings.Cut(sc.Text(), " => "); ok {
			rows[typed] = row
		}
	}
	return rows
}

// Seen live on 2.3.0: the / menu listed six commands, none of the
// terminal's own (/new, /themes, /help), and /new was refused. The
// terminal's commands are not on the server: they are listed here and typed.
//
// A typed command is run by the terminal's menu: Enter takes the menu's
// first row. testdata/v2/tui-slash-home.txt and tui-slash-session.txt are
// those first rows on a 2.2.5 terminal, before and inside a conversation,
// for every name upstream defines. They show why some names are left out:
// one the screen does not have is matched to another command ("/redo" ran
// /reload, "/rename" put /review in the input line) or stays in the line.
// So every listed command must be the first row on both screens.
func TestPaletteListsTheTerminalsOwnCommands(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	home, inSession := slashRows(t, "tui-slash-home.txt"), slashRows(t, "tui-slash-session.txt")
	if len(home) != 38 || len(inSession) != 38 {
		t.Fatalf("recordings hold %d and %d names, want 38", len(home), len(inSession))
	}
	var typed, native []string
	for _, c := range a.Commands.Static {
		if a.CommandKind(c) != spec.CommandTerminal {
			native = append(native, c.Name)
			continue
		}
		typed = append(typed, c.Name)
		if !a.TypedCommand(c.Name) {
			t.Errorf("/%s is not typed into the terminal", c.Name)
		}
		if c.Detail == "" {
			t.Errorf("/%s has no description", c.Name)
		}
		for where, rows := range map[string]map[string]string{"home": home, "session": inSession} {
			if row := rows[c.Name]; !strings.HasPrefix(row, "/"+c.Name+" ") {
				t.Errorf("/%s on the %s screen runs %q", c.Name, where, row)
			}
		}
	}
	slices.Sort(typed)
	want := "agents clear connect debug editor exit help new open pair plugins reload settings skills stats status themes usage variants worktrees"
	if got := strings.Join(typed, " "); got != want {
		t.Errorf("typed commands\n%s\nwant\n%s", got, want)
	}
	// The ones with a native home open it instead.
	for _, name := range []string{"models", "sessions", "compact", "mcps", "diff"} {
		if !slices.Contains(native, name) {
			t.Errorf("/%s has no native home", name)
		}
	}
	// The server's commands are not typed: the command route takes them.
	for _, name := range []string{"init", "review", "goal"} {
		if a.TypedCommand(name) {
			t.Errorf("/%s is the server's and would be typed", name)
		}
	}
	if _, ok := a.Actions["command"]; !ok {
		t.Error("the command action is gone: server commands have no route")
	}
}
