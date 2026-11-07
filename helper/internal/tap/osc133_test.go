package tap

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func marks(t *testing.T, signals []wire.Signal) []Mark {
	t.Helper()
	var out []Mark
	for _, s := range signals {
		if s.Kind != SignalShell {
			continue
		}
		var m Mark
		if err := json.Unmarshal([]byte(s.Body), &m); err != nil {
			t.Fatalf("mark body %q: %v", s.Body, err)
		}
		if s.Title != m.Mark {
			t.Fatalf("signal title %q, mark %q", s.Title, m.Mark)
		}
		out = append(out, m)
	}
	return out
}

func code(n int) *int { return &n }

// A zsh-like session: OSC 7 cwd, prompt, the echoed command line, its colored
// output, and the end mark with status.
const shellSession = "\x1b]7;file://mac/Users/me/src\a" +
	"\x1b]133;A\a~/src %\x1b]133;B\a" +
	"ls -l" + "\r\n" +
	"\x1b]133;C\a" +
	"\x1b[1;34mdir\x1b[0m\r\nfile.txt\r\n" +
	"\x1b]133;D;0\a" +
	"\x1b]133;A\a~/src %\x1b]133;B\a"

func TestShellBlockFromEcho(t *testing.T) {
	got := marks(t, feedAll(shellSession))
	want := []Mark{
		{Mark: MarkStart, Command: "ls -l", Cwd: "/Users/me/src"},
		{Mark: MarkEnd, Command: "ls -l", Cwd: "/Users/me/src", Exit: code(0), Output: "dir\nfile.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestShellMarksSplitAnywhere(t *testing.T) {
	want := marks(t, feedAll(shellSession))
	for i := 0; i <= len(shellSession); i++ {
		got := marks(t, feedAll(shellSession[:i], shellSession[i:]))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split at %d: got %+v", i, got)
		}
	}
	var s Scanner
	var all []wire.Signal
	for i := 0; i < len(shellSession); i++ {
		all = append(all, s.Feed([]byte{shellSession[i]})...)
	}
	if got := marks(t, all); !reflect.DeepEqual(got, want) {
		t.Fatalf("byte at a time: got %+v", got)
	}
}

func TestShellExitCodes(t *testing.T) {
	got := marks(t, feedAll("\x1b]133;B\afalse\x1b]133;C\a\x1b]133;D;1\a"))
	if len(got) != 2 || got[1].Exit == nil || *got[1].Exit != 1 {
		t.Fatalf("got %+v", got)
	}
	// Parameters after the status, as some integrations add.
	got = marks(t, feedAll("\x1b]133;B\ax\x1b]133;C\a\x1b]133;D;130;aid=7\a"))
	if len(got) != 2 || got[1].Exit == nil || *got[1].Exit != 130 {
		t.Fatalf("got %+v", got)
	}
	// No status at all.
	got = marks(t, feedAll("\x1b]133;B\ax\x1b]133;C\a\x1b]133;D\a"))
	if len(got) != 2 || got[1].Exit != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestShellMissingMarks(t *testing.T) {
	// No end mark: the next prompt ends the block, status unknown.
	got := marks(t, feedAll("\x1b]133;B\asleep 9\x1b]133;C\aZzz\x1b]133;A\a"))
	want := []Mark{
		{Mark: MarkStart, Command: "sleep 9"},
		{Mark: MarkEnd, Command: "sleep 9", Output: "Zzz"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	// No output mark: the end mark alone makes a finished block.
	got = marks(t, feedAll("\x1b]133;A\a$ \x1b]133;B\apwd\r\n/tmp\r\n\x1b]133;D;0\a"))
	if len(got) != 1 || got[0].Mark != MarkEnd || got[0].Command != "pwd" || *got[0].Exit != 0 {
		t.Fatalf("got %+v", got)
	}
	// An empty line at the prompt makes nothing.
	if got := marks(t, feedAll("\x1b]133;A\a$ \x1b]133;B\a\r\n\x1b]133;D\a")); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	// Marks without a prompt never start a block, and an end alone is nothing.
	if got := marks(t, feedAll("\x1b]133;D;0\a")); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestShellExplicitCommand(t *testing.T) {
	// VS Code's E mark, escaped, beats the echo.
	got := marks(t, feedAll("\x1b]633;B\agit st\x1b]633;E;git status \\x3b echo a\\\\b\a\x1b]633;C\a\x1b]633;D;0\a"))
	if len(got) != 2 || got[0].Command != `git status ; echo a\b` {
		t.Fatalf("got %+v", got)
	}
	// kitty and fish send it on C.
	got = marks(t, feedAll("\x1b]133;B\a\x1b]133;C;cmdline_url=make%20test\a\x1b]133;D;2\a"))
	if len(got) != 2 || got[0].Command != "make test" {
		t.Fatalf("got %+v", got)
	}
	// A C mark too long to keep still starts the block.
	long := "\x1b]133;B\aecho\x1b]133;C;cmdline=" + strings.Repeat("x", 3000) + "\aok\x1b]133;D;0\a"
	got = marks(t, feedAll(long))
	if len(got) != 2 || got[0].Command != "echo" || got[1].Output != "ok" {
		t.Fatalf("got %+v", got)
	}
}

func TestShellLineEditing(t *testing.T) {
	// Backspace, a cursor move back and an erase to the end of the line, as
	// line editors redraw.
	got := marks(t, feedAll("\x1b]133;B\agti\b\b\bgit lgo\x1b[2Dog\x1b[K\x1b]133;C\a\x1b]133;D;0\a"))
	if len(got) != 2 || got[0].Command != "git log" {
		t.Fatalf("got %+v", got)
	}
	// Multi-byte characters typed in pieces.
	got = marks(t, feedAll("\x1b]133;B\aecho h\xc3", "\xa9\x1b]133;C\a\x1b]133;D\a"))
	if len(got) != 2 || got[0].Command != "echo hé" {
		t.Fatalf("got %+v", got)
	}
}

func TestShellOutputSettlesRedraws(t *testing.T) {
	got := marks(t, feedAll("\x1b]133;B\ax\x1b]133;C\a10%\r50%\r100%\r\ndone\x07\r\n\x1b]133;D;0\a"))
	if len(got) != 2 || got[1].Output != "100%\ndone" {
		t.Fatalf("got %+v", got)
	}
}

func TestShellOutputTailBounded(t *testing.T) {
	var s Scanner
	s.Feed([]byte("\x1b]133;B\ayes\x1b]133;C\a"))
	for range 200 {
		s.Feed([]byte(strings.Repeat("y\r\n", 500)))
	}
	if cap(s.shell.out) > 4*MaxOutput {
		t.Fatalf("output buffer grew to %d", cap(s.shell.out))
	}
	got := marks(t, s.Feed([]byte("é\x1b]133;D;0\a")))
	if len(got) != 1 || len(got[0].Output) > MaxOutput+2 || !strings.HasSuffix(got[0].Output, "y\né") {
		t.Fatalf("got %d bytes", len(got[0].Output))
	}
}

func TestShellSignalsKeepBells(t *testing.T) {
	got := feedAll("\x1b]133;B\ax\x1b]133;C\a\a\x1b[5\aA\x1b]133;D;0\a")
	bells := 0
	for _, s := range got {
		if s.Kind == wire.SignalBell {
			bells++
		}
	}
	if bells != 2 {
		t.Fatalf("got %+v", got)
	}
}
