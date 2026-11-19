package sessiongen

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Aider writes Aider's chat history: .aider.chat.history.md in the folder
// (or at its git root), markdown that every run of aider in the folder
// appends to (adapters/aider/aider.yaml, history.go). The format is
// aider's own io.py: a "# aider chat started at <local time>" header per
// run, "#### text  " per prompt line, "> text  " per line of tool output,
// and the reply as plain markdown. The file has no turn end: a prompt
// opens a turn, and the next prompt or the pane going idle closes it.
//
// Every tool line the generator writes is one of the lines in
// adapters/aider/testdata (chat.history.md is aider's own InputOutput
// writing a history; the real-*.history.md files are real runs), with
// file names, hashes and numbers varied; the honesty test checks that.
// Not generated, for want of a recording: an interrupt (the ^C line is
// written from aider's source in source-limits.history.md, not captured),
// a subagent, a background job, an exit and a compaction.
type Aider struct {
	// Extra adds Aider-only content to a turn, keyed by the number of the
	// turn's prompt (1 is the first).
	Extra map[int]AiderTurn

	path    string
	started bool
}

// AiderTurn is what an Aider turn can have besides the common script
// fields.
type AiderTurn struct {
	// Files adds this many "Added X to the chat" lines before the reply.
	Files int
	// Edits is the number of "Applied edit to X" lines after the reply;
	// Commit adds the commit line aider makes after them.
	Edits  int
	Commit bool
	// Unfinished writes the prompt and nothing else: aider is at work.
	Unfinished bool
}

func (a *Aider) Name() string { return "aider" }

// runs are commands whose "Running <command>" line is in the recordings.
var aiderRuns = []string{"python hello.py", "echo one"}

func (a *Aider) Open(g *Gen) error {
	a.path = filepath.Join(g.Cwd, ".aider.chat.history.md")
	g.L.Mains = append(g.L.Mains, a.path)
	return a.header(g)
}

// header starts a run: the header with the local time of the clock, and
// the lines aider prints on starting (the launch line is dropped by the
// mapper, the others become one step).
func (a *Aider) header(g *Gen) error {
	at := g.Tick()
	lines := []string{"", "# aider chat started at " + at.In(time.Local).Format("2006-01-02 15:04:05"), "",
		"> " + filepath.Join(g.Home, ".local", "bin", "aider") + " --no-check-update --no-show-release-notes  ",
		"> Aider v0.86.2  ",
		"> Model: openai/qwen3.8-27b with diff edit format  ",
		"> Git repo: .git with 0 files  ",
		"> Repo-map: using 4096 tokens, auto refresh  ", ""}
	return a.lines(g, at, lines...)
}

func (a *Aider) lines(g *Gen, at time.Time, lines ...string) error {
	for _, l := range lines {
		if err := g.EmitRaw(a.path, at, l); err != nil {
			return err
		}
	}
	return nil
}

func (a *Aider) Turn(g *Gen, t Turn) error {
	if t.Agents != nil || t.Jobs > 0 || t.Exit || t.Interrupt || t.Compact {
		return fmt.Errorf("sessiongen: the Aider profile writes no subagents, jobs, exit, interrupt or compaction: no real recording of them")
	}
	if t.New {
		if err := a.header(g); err != nil {
			return err
		}
	}
	n := g.L.Truth.Prompts + 1
	x := a.Extra[n]
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", n, g.Text(40))
	}
	g.L.Truth.Prompts++
	at := g.Tick()
	if err := a.lines(g, at, "#### "+prompt+"  "); err != nil {
		return err
	}
	if x.Unfinished {
		return nil
	}
	if t.Error {
		// The model server refuses the request (real-model-not-found).
		return a.lines(g, g.Tick(), "> litellm.BadRequestError: OpenAIException - no router for requested model  ", "")
	}
	for i := 0; i < x.Files; i++ {
		if err := a.lines(g, g.Tick(), fmt.Sprintf("> Added f%d_%d.py to the chat  ", n, i)); err != nil {
			return err
		}
	}
	reply := []string{"", wrap(g.Text(max(t.Say, 1)), 72), ""}
	if x.Edits > 0 {
		reply = append(reply, "hello.py", "```python", "<<<<<<< SEARCH", "=======", "def hello():", "    print(\"hi\")", ">>>>>>> REPLACE", "```", "")
	}
	if err := a.lines(g, g.Tick(), reply...); err != nil {
		return err
	}
	tail := []string{fmt.Sprintf("> Tokens: %d.%dk sent, %d received.  ", 1+g.Intn(9), g.Intn(10), 20+g.Intn(200))}
	for i := 0; i < x.Edits; i++ {
		tail = append(tail, fmt.Sprintf("> Applied edit to f%d_%d.py  ", n, i))
	}
	if x.Commit {
		tail = append(tail, "> Commit "+g.Hex(7)+" feat: add hello function  ")
	}
	for i := 0; i < t.Tools; i++ {
		tail = append(tail, "> Running "+aiderRuns[(n+i)%len(aiderRuns)]+"  ")
		if t.Output > 0 {
			tail = append(tail, "> Added 1 line of output to the chat.  ")
		}
	}
	return a.lines(g, g.Tick(), append(tail, "")...)
}

// wrap breaks text into lines of about width bytes, as a reply is.
func wrap(s string, width int) string {
	var out []string
	var cur string
	for _, w := range strings.Fields(s) {
		if len(cur)+len(w) >= width && cur != "" {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += " "
		}
		cur += w
	}
	return strings.Join(append(out, cur), "\n")
}
