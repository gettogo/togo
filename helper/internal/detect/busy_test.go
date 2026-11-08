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
	if !KnowsBusy("claude") || !KnowsBusy("codex") || KnowsBusy("aider") || KnowsBusy("copilot") {
		t.Error("KnowsBusy")
	}
}

// omp 18.16 during a turn and at rest (captured live, path shortened).
const ompWorking = ` Count slowly from 1 to 30, one number per line.
  ⎋ Working…
 ⠦ 0s > ◑ Haiku 4.5 > 🗑 …/ompw > (sub) ▶──6%──────────────────────┃────200K─
╰─
`

const ompIdle = ` 29
 30
 π > ◑ Haiku 4.5 > 🗑 …/ompw > S0.01 ▶──7%─────────────────────────┃────200K─
╰─
`

func TestBusyOmp(t *testing.T) {
	if !KnowsBusy("omp") || !Busy("omp", ompWorking) || Busy("omp", ompIdle) {
		t.Fatalf("omp: knows %v, working %v, idle %v", KnowsBusy("omp"), Busy("omp", ompWorking), Busy("omp", ompIdle))
	}
}
