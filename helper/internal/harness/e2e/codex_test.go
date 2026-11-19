package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Codex, read through its app-server: the engine starts the test
// app-server (enginetest.CodexServer) as it starts the real one, and the
// Codex profile's journal is what Codex does (sessiongen/codex.go).

type codexSession struct {
	e  env
	p  *sessiongen.Codex
	l  *sessiongen.Layout
	r  *enginetest.Rig
	s  *enginetest.CodexServer
	mu sync.Mutex // Append from the terminal's prompt hook and the test
}

func codexStart(t *testing.T, seed int64, mod func(*enginetest.Options), turns ...sessiongen.Turn) *codexSession {
	t.Helper()
	e := newEnv(t, 10*time.Minute)
	p := &sessiongen.Codex{}
	// The server sends a message as it is written: its times are the
	// present, a step apart.
	l, err := sessiongen.Write(e.home, p, sessiongen.Script{Seed: seed, Start: time.Now().UTC(), Gap: time.Millisecond, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	o := enginetest.Options{Agent: "codex", NoRedact: true}
	if mod != nil {
		mod(&o)
	}
	r := e.rigWith(t, o)
	return &codexSession{e: e, p: p, l: l, r: r, s: r.Codex()}
}

// add writes more turns: Codex at work.
func (c *codexSession) add(t *testing.T, turns ...sessiongen.Turn) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	now(c.l)
	if err := c.l.Append(turns...); err != nil {
		t.Fatal(err)
	}
}

// hold writes a turn and holds back what follows the first message that
// stop picks (or the turn's first message, for nil): Codex is at work on
// it, or waits, until release.
func (c *codexSession) hold(t *testing.T, turn sessiongen.Turn, stop func(m map[string]any) bool) (release func()) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.p.Journal()
	now(c.l)
	before, _ := os.ReadFile(path)
	if err := c.l.Append(turn); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	tail := after[len(before):]
	cut := 0
	for cut < len(tail) {
		j := bytes.IndexByte(tail[cut:], '\n')
		var m map[string]any
		_ = json.Unmarshal(tail[cut:cut+j], &m)
		cut += j + 1
		if stop == nil || stop(m) {
			break
		}
	}
	if err := os.WriteFile(path, after[:len(before)+cut], 0o644); err != nil {
		t.Fatal(err)
	}
	held := append([]byte(nil), tail[cut:]...)
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			_, _ = f.Write(held)
		})
	}
}

// method picks a message by its method.
func method(name string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["method"] == name }
}

// answerPrompts makes Codex run a short turn on the thread it shows for
// each prompt typed into its terminal, as the terminal client does.
func (c *codexSession) answerPrompts() {
	c.r.Term.OnSubmit = func(text string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		now(c.l)
		_ = c.l.Append(sessiongen.Turn{Prompt: text, Say: 12})
	}
}

// promptsIn lists the prompts each thread's history holds, as the server
// would return them.
func promptsOn(st enginetest.State, text string) []harness.Item {
	return items(st, func(it harness.Item) bool { return it.User != nil && it.User.Text == text })
}

// (a) A long session: many turns with commands, file changes and
// subagents (plans: TestKnownFault_Codex_RestartLosesPlan), written in pieces while the helper reads it, with (b) a
// helper restart after every piece but the last. Each restart finds the
// same app-server, joins the thread again and shows what it showed; the
// rules hold everywhere; the session crosses several history pages.
func TestCodexLongSession(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/scale/long_session", "codex/lifecycle/helper_restart",
		"codex/capability/chat", "codex/capability/chat.history", "codex/capability/agents",
		"codex/capability/changes")
	n := scale(20, 200)
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 40 + i%5*30, Tools: 1 + i%3, Output: 80}
		if i%5 == 4 {
			turns[i].Agents = &sessiongen.Batch{Size: 2, Steps: 1}
		}
	}
	c := codexStart(t, 61, nil, turns[:3]...)
	points := []int{3}
	for _, p := range cuts(61, scale(3, 8), 4, n-1) {
		points = append(points, p)
	}
	st := drive(t, c.r, c.l, turns, points)
	st = c.r.SettleUntil(driveLimit, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	if len(st.Pages) < 3 {
		t.Errorf("%d history pages, want several", len(st.Pages))
	}
	if got := len(userPrompts(st)); got != n {
		t.Errorf("%d prompts, want %d", got, n)
	}
	agents := map[string]bool{}
	for _, it := range items(st, ofKind(harness.KindAgent)) {
		agents[it.Agent.AgentID] = true
		if it.Agent.State != harness.StateOK {
			t.Errorf("subagent %s is %s", it.ID, it.Agent.State)
		}
	}
	if len(agents) != len(c.l.Truth.Agents) {
		t.Errorf("%d subagents shown, %d ran", len(agents), len(c.l.Truth.Agents))
	}
	rows := map[string]int{}
	for _, sec := range st.Mission.Sections {
		rows[sec.Kind] += len(sec.Rows)
	}
	if rows[harness.SectionChanges] == 0 {
		t.Error("no changes shown")
	}
	nothingRuns(t, st)
	st = restoreTwice(t, c.r, c.l, n)
	for _, size := range []int{7, 50} {
		fail(t, "paging", enginetest.SamePaging(st, c.r.ReadPages(size)))
	}
}

// A session over the engine's item log keeps the newest items, says it
// dropped the older ones, and pages the same at any size.
func TestCodexAtLimit(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/scale/at_limit")
	turns := make([]sessiongen.Turn, 20)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 30, Tools: 2, Output: 40}
	}
	c := codexStart(t, 63, func(o *enginetest.Options) { o.MaxItems = 60 }, turns...)
	st := settleUntil(c.r, len(turns))
	if st.Discarded == 0 || len(st.Items) > 60 {
		t.Errorf("%d items kept, discarded through %d", len(st.Items), st.Discarded)
	}
	checkEdge(t, "at the limit", st, c.l)
	fail(t, "paging", enginetest.SamePaging(st, c.r.ReadPages(7)))
	fail(t, "paging", enginetest.Paging(c.r.ReadPages(3)))
}
