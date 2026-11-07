package detect

import (
	"strings"
	"testing"
)

const claudeIdle = `╭───────────────────────────────────────────╮
│ ✻ Welcome to Claude Code!                 │
│   cwd: /Users/me/project                  │
╰───────────────────────────────────────────╯

╭───────────────────────────────────────────╮
│ >                                         │
╰───────────────────────────────────────────╯
  ? for shortcuts
`

const claudeWorking = `> fix the tests

✻ Pondering… (12s · ↑ 1.2k tokens · esc to interrupt)

╭───────────────────────────────────────────╮
│ >                                         │
╰───────────────────────────────────────────╯
  Opus · 34% context
`

const codexWorking = `› fix the tests

• Working (5s • Esc to interrupt)

› Ask Codex to do anything
  ⏎ send   ⌃J newline   ⌃T transcript
`

const geminiWorking = `> fix the tests

⠏ Reading the test files (esc to cancel, 12s)

╭────────────────────────────────────────╮
│ >   Type your message or @path/to/file │
╰────────────────────────────────────────╯
~/project      no sandbox      gemini-2.5-pro
`

const geminiDialog = `Allow execution of: 'go test ./...'?

● 1. Yes, allow once
  2. Yes, allow always
  3. No, suggest changes (esc)
(Use Enter to select, Esc to cancel)
`

const piWorking = `fix the tests

⠋ Working... (escape to interrupt)
────────────────────
>
`

const piIdle = `pi v0.73
escape to interrupt · ctrl+c to clear · / for commands
────────────────────
>
`

func TestBusy(t *testing.T) {
	cases := []struct {
		agent, screen string
		want          bool
	}{
		{"claude", claudeIdle, false},
		{"claude", claudeWorking, true},
		{"codex", codexWorking, true},
		{"codex", "› Ask Codex to do anything\n", false},
		{"aider", claudeWorking, false},
		{"gemini", geminiWorking, true},
		{"gemini", geminiDialog, false},
		{"auggie", "Working (5s • esc to interrupt)\n> \n", true},
		{"auggie", "> \n? for shortcuts\n", false},
		{"pi", piWorking, true},
		{"pi", piIdle, false},
		{"", claudeWorking, false},
		// An indicator scrolled far up is history, not the current turn.
		{"claude", claudeWorking + strings.Repeat("line\n", 20), false},
	}
	for i, c := range cases {
		if got := Busy(c.agent, c.screen); got != c.want {
			t.Errorf("case %d (%s): Busy = %v, want %v", i, c.agent, got, c.want)
		}
	}
	if !KnowsBusy("claude") || !KnowsBusy("codex") || !KnowsBusy("gemini") || KnowsBusy("aider") || KnowsBusy("copilot") {
		t.Error("KnowsBusy")
	}
}
