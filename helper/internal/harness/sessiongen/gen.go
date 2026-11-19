// Package sessiongen writes an agent's on-disk session records from a small
// seeded script, the way that agent writes them, so the real adapters and
// engine can be run over long sessions without a model.
//
// Each profile writes only record types and fields that the agent's real
// recordings in adapters/<agent>/testdata hold and that the adapter's
// mapper reads. The honesty tests in this package compare the two.
package sessiongen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Script is what happens in a session, turn by turn.
type Script struct {
	// Seed makes ids, texts and subagent arrival order repeatable: the
	// same script and seed write the same bytes.
	Seed int64
	// Start is the time of the first record. Gap is the time between two
	// records (default 2 s).
	Start time.Time
	Gap   time.Duration
	// Cwd is the folder the agent runs in; it must lie under the home
	// folder passed to Write.
	Cwd   string
	Turns []Turn
}

// Turn is one user prompt and what the agent does with it.
type Turn struct {
	// Prompt is the user's text; empty writes a generated one.
	Prompt string
	// Say is the size in bytes of the reply that ends the turn.
	Say int
	// Tools is the number of tool calls (each with its result) before the
	// reply; Output is the size in bytes of each result.
	Tools  int
	Output int
	// Agents starts a batch of subagents in this turn.
	Agents *Batch
	// Jobs is the number of background shell jobs the turn starts. Each
	// settles before the turn's reply.
	Jobs int
	// Compact writes a compaction before the prompt.
	Compact bool
	// New starts a new conversation file before the prompt (the agent's
	// /new): later records go to the new file.
	New bool
	// Interrupt ends the turn interrupted by the user, with no reply.
	// Error ends it with a failed model request.
	Interrupt bool
	Error     bool
	// Exit records the agent's own exit after the turn.
	Exit bool
	// Hook holds what only hook-driven agents (copilot, droid, cursor,
	// amp) do; the other profiles ignore it.
	Hook *HookTurn
}

// Batch is a group of subagents started by one call.
type Batch struct {
	// Size is the number of subagents; Steps the tool calls each makes.
	Size  int
	Steps int
	// Background returns the call at once; each agent is reported later.
	// A foreground call returns when they are all done.
	Background bool
	// ReportLater writes omp's report of each background agent at the
	// start of the next turn instead of this one.
	ReportLater bool
	// Unreported is how many of a background batch's agents the main
	// conversation never reports, as when the agent is still at work
	// and its file ends without a reply or an exit.
	Unreported int
}

// AgentTruth is what the generator knows about one subagent.
type AgentTruth struct {
	// Task is the name the main conversation's call gives the agent
	// (omp's task id), or the agent's id (Claude Code's agentId).
	Task string
	// Prompt is the first user text in the agent's own file.
	Prompt string
	File   string
	// Exited is true when the agent's file records its exit.
	Exited bool
	// Settled is true when the main conversation reports its end.
	Settled bool
}

// Truth is what was written, for tests to compare with what the engine
// reports.
type Truth struct {
	Prompts int
	Agents  []AgentTruth
	Jobs    []string
	// Exited is true once the agent recorded its own exit.
	Exited bool
	// Last is the id of the main-conversation item the last record
	// written makes: the newest reply, or the notice of an interrupted or
	// failed turn. Tests wait for it.
	Last string
}

// Layout lists the files written. Append continues the same session.
type Layout struct {
	// Mains are the main conversation files, oldest first: one more for
	// each /new.
	Mains []string
	// Subagents are the subagent files in the order they were written.
	Subagents []string
	// Terminal is omp's record of the file the terminal shows; empty for
	// agents that keep none.
	Terminal string
	// Records counts the lines written to each file.
	Records map[string]int
	Truth   Truth
	// Hooks are the hook calls of a hook-driven agent, in the order the
	// agent ran them. A test delivers them, as `togo hook` would.
	Hooks []HookCall

	g *Gen
	p Profile
}

// Main is the conversation file written last.
func (l *Layout) Main() string { return l.Mains[len(l.Mains)-1] }

// Profile writes records the way one agent does.
type Profile interface {
	// Name is the adapter's name ("omp", "claude").
	Name() string
	// Open starts the first conversation file.
	Open(g *Gen) error
	// Turn writes one turn.
	Turn(g *Gen, t Turn) error
}

// Gen is the generator's state, shared by the profiles: the clock, the
// seeded random source and the files being written.
type Gen struct {
	Home   string
	Cwd    string
	L      *Layout
	rnd    *rand.Rand
	now    time.Time
	gap    time.Duration
	bufs   map[string]*bytes.Buffer
	order  []string
	last   map[string]time.Time
	serial int
}

// Write writes the script's session under home (the user's home folder)
// and returns the files it wrote.
func Write(home string, p Profile, s Script) (*Layout, error) {
	if s.Gap <= 0 {
		s.Gap = 2 * time.Second
	}
	if s.Start.IsZero() {
		return nil, fmt.Errorf("sessiongen: script has no start time")
	}
	if rel, err := filepath.Rel(home, s.Cwd); err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("sessiongen: cwd %q is not under %q", s.Cwd, home)
	}
	l := &Layout{Records: map[string]int{}, p: p}
	g := &Gen{Home: home, Cwd: s.Cwd, L: l, rnd: rand.New(rand.NewSource(s.Seed)), now: s.Start.UTC(), gap: s.Gap,
		bufs: map[string]*bytes.Buffer{}, last: map[string]time.Time{}}
	l.g = g
	if err := p.Open(g); err != nil {
		return nil, err
	}
	return l, l.Append(s.Turns...)
}

// Finisher is a profile that has more to write once the turns of one
// Append are done: an agent that rewrites a whole file (Cline) writes it
// here, once, not after every turn.
type Finisher interface {
	Finish(g *Gen) error
}

// Append writes more turns to the session, as the agent keeps working.
func (l *Layout) Append(turns ...Turn) error {
	for _, t := range turns {
		if err := l.p.Turn(l.g, t); err != nil {
			return err
		}
	}
	if f, ok := l.p.(Finisher); ok {
		if err := f.Finish(l.g); err != nil {
			return err
		}
	}
	return l.g.flush()
}

// Now is the generator's clock: the time the next record gets.
func (l *Layout) Now() time.Time { return l.g.now }

// Skip moves the generator's clock forward, for a pause between turns.
func (l *Layout) Skip(d time.Duration) { l.g.now = l.g.now.Add(d) }

// Age sets every written file's modification time back by d from the
// time of its last record.
func (l *Layout) Age(d time.Duration) error {
	for p, at := range l.g.last {
		t := at.Add(-d)
		if err := os.Chtimes(p, t, t); err != nil {
			return err
		}
	}
	return nil
}

// Tick returns the time of the next record and moves the clock on.
func (g *Gen) Tick() time.Time {
	at := g.now
	g.now = g.now.Add(g.gap)
	return at
}

// Hex returns n random lowercase hex digits.
func (g *Gen) Hex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = digits[g.rnd.Intn(16)]
	}
	return string(b)
}

// Serial returns a number that grows by one on each call.
func (g *Gen) Serial() int {
	g.serial++
	return g.serial
}

// Intn returns a seeded number in [0, n).
func (g *Gen) Intn(n int) int { return g.rnd.Intn(n) }

// Shuffle returns 0..n-1 in a seeded order.
func (g *Gen) Shuffle(n int) []int { return g.rnd.Perm(n) }

var words = strings.Fields("the parser reads each record and keeps the newest state for the session " +
	"so a test can check what the engine reports after a restart of the helper process")

// Text returns n bytes of plain prose (at least one byte), so a test can
// size a file to the byte.
func (g *Gen) Text(n int) string {
	n = max(n, 1)
	var b strings.Builder
	for b.Len() < n {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[g.rnd.Intn(len(words))])
	}
	s := b.String()[:n]
	if strings.HasSuffix(s, " ") {
		s = s[:n-1] + "s"
	}
	return s
}

// Emit appends one record to a file.
func (g *Gen) Emit(path string, at time.Time, rec any) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return g.EmitRaw(path, at, string(raw))
}

// EmitRaw appends one line of text to a file: for agents whose history is
// not JSON (Aider's markdown).
func (g *Gen) EmitRaw(path string, at time.Time, line string) error {
	raw := []byte(line)
	b := g.bufs[path]
	if b == nil {
		b = &bytes.Buffer{}
		g.bufs[path] = b
		g.order = append(g.order, path)
	}
	b.Write(raw)
	b.WriteByte('\n')
	g.L.Records[path]++
	if at.After(g.last[path]) {
		g.last[path] = at
	}
	return nil
}

// Replace writes a whole file, as an agent rewrites a small record file.
func (g *Gen) Replace(path string, at time.Time, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	g.last[path] = at
	return os.Chtimes(path, at, at)
}

// flush appends the buffered records to their files, in the order the
// files were first written to, and dates each file by its last record.
func (g *Gen) flush() error {
	for _, p := range g.order {
		b := g.bufs[p]
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write(b.Bytes())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if err := os.Chtimes(p, g.last[p], g.last[p]); err != nil {
			return err
		}
	}
	g.bufs, g.order = map[string]*bytes.Buffer{}, nil
	return nil
}
