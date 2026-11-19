package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Scenarios for the agents the helper drives through their own HTTP API
// and event stream (OpenCode 1.x, Kilo Code): the rig launches them on the
// port and password the engine picks, and a test API server plays the
// generated stream (enginetest.APIServer, sessiongen.OpenCode). Each runs
// for both agents; opencode_test.go and kilo_test.go declare the cells.

// apiSession is a generated session served by the test API server.
type apiSession struct {
	e   env
	p   *sessiongen.OpenCode
	l   *sessiongen.Layout
	r   *enginetest.Rig
	api *enginetest.APIServer
	mu  sync.Mutex // Append from the agent's prompt hook and the test
}

func apiStart(t *testing.T, agent string, seed int64, mod func(*enginetest.Options), turns ...sessiongen.Turn) *apiSession {
	t.Helper()
	e := newEnv(t, 10*time.Minute)
	p := &sessiongen.OpenCode{Kilo: agent == "kilo"}
	// The agent's events carry the times they happen at, and the stream
	// brings them at once: the clock is the present, a step apart.
	l, err := sessiongen.Write(e.home, p, sessiongen.Script{Seed: seed, Start: time.Now().UTC(), Gap: time.Millisecond, Cwd: e.cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	o := enginetest.Options{Agent: agent, NoRedact: true}
	if mod != nil {
		mod(&o)
	}
	r := e.rigWith(t, o)
	s := &apiSession{e: e, p: p, l: l, r: r, api: r.API()}
	if s.api == nil {
		t.Fatalf("%s is not served through an API server", agent)
	}
	return s
}

// add writes more turns, as the agent working.
func (s *apiSession) add(t *testing.T, turns ...sessiongen.Turn) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	now(s.l)
	if err := s.l.Append(turns...); err != nil {
		t.Fatal(err)
	}
}

// hold writes a turn and holds back all of it but its first n events
// (the prompt): the agent is at work on it until release.
func (s *apiSession) hold(t *testing.T, n int, turn sessiongen.Turn) (release func()) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.api.Journal()
	now(s.l)
	before, _ := os.ReadFile(path)
	if err := s.l.Append(turn); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	tail := after[len(before):]
	cut := 0
	for i := 0; i < n; i++ {
		j := strings.IndexByte(string(tail[cut:]), '\n')
		if j < 0 {
			t.Fatalf("the turn has fewer than %d events", n)
		}
		cut += j + 1
	}
	if err := os.WriteFile(path, after[:len(before)+cut], 0o644); err != nil {
		t.Fatal(err)
	}
	held := append([]byte(nil), tail[cut:]...)
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(held); err != nil {
			t.Fatal(err)
		}
	}
}

// answerPrompts makes the agent run a short turn for each prompt it takes
// through its API, as the real agent does.
func (s *apiSession) answerPrompts(t *testing.T) {
	s.r.Term.OnSubmit = func(text string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		now(s.l)
		_ = s.l.Append(sessiongen.Turn{Prompt: text, Say: 12})
	}
}

// prompts lists the prompt calls the agent was given: the session each
// went to and its text.
func (s *apiSession) prompts() [][2]string {
	var out [][2]string
	for _, c := range s.api.Posts() {
		if !strings.HasSuffix(c.Path, "/prompt_async") || c.Status/100 != 2 {
			continue
		}
		var body struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		_ = json.Unmarshal([]byte(c.Body), &body)
		sess := strings.TrimSuffix(strings.TrimPrefix(c.Path, "/session/"), "/prompt_async")
		if len(body.Parts) > 0 {
			out = append(out, [2]string{sess, body.Parts[0].Text})
		}
	}
	return out
}

// waitPost waits for a call to a path ending in suffix and returns it.
func (s *apiSession) waitPost(t *testing.T, suffix string) enginetest.APICall {
	t.Helper()
	c, ok := s.api.WaitCall(5*time.Second, func(c enginetest.APICall) bool {
		return c.Method != "GET" && strings.HasSuffix(strings.SplitN(c.Path, "?", 2)[0], suffix)
	})
	if !ok {
		t.Fatalf("no call to %s: %+v", suffix, s.api.Posts())
	}
	return c
}

// apiLong: a long session of many turns with tool steps, subagent
// sessions, a plan and changed files, written in pieces while the helper
// reads it, past several history pages. Every check of the engine's rules
// holds after each piece; the subagents, plan and changes are shown.
func apiLong(t *testing.T, agent string) {
	n := scale(24, 240)
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 40 + i%5*30, Tools: 1 + i%2, Output: 60}
		if i%4 == 3 {
			turns[i].Agents = &sessiongen.Batch{Size: 2, Steps: 1}
		}
	}
	s := apiStart(t, agent, 51, nil, turns[:4]...)
	st := settleUntil(s.r, 4)
	checkEdge(t, "start", st, s.l)
	for done := 4; done < n; {
		next := min(done+scale(5, 40), n)
		s.add(t, turns[done:next]...)
		done = next
		st = settleUntil(s.r, done)
		checkEdge(t, "live", st, s.l)
	}
	st = s.r.SettleUntil(driveLimit, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	if len(st.Pages) < 3 {
		t.Errorf("%d history pages, want the session to cross several", len(st.Pages))
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
	if len(agents) != len(s.l.Truth.Agents) {
		t.Errorf("%d subagents shown, %d ran", len(agents), len(s.l.Truth.Agents))
	}
	rows := map[string]int{}
	for _, sec := range st.Mission.Sections {
		rows[sec.Kind] += len(sec.Rows)
	}
	// Finished subagents leave the agents section; their items stay.
	for _, k := range []string{harness.SectionPlan, harness.SectionChanges} {
		if rows[k] == 0 {
			t.Errorf("no %s shown", k)
		}
	}
	nothingRuns(t, st)
	for _, size := range []int{7, 50} {
		fail(t, "paging", enginetest.SamePaging(st, s.r.ReadPages(size)))
	}
}

// nothingRuns: no item is left running or waiting, and the summary is
// not working.
func nothingRuns(t *testing.T, st enginetest.State) {
	t.Helper()
	for _, it := range items(st, func(harness.Item) bool { return true }) {
		var state string
		switch {
		case it.Turn != nil:
			state = it.Turn.State
		case it.Tool != nil:
			state = it.Tool.State
		case it.Agent != nil:
			state = it.Agent.State
		}
		if state == harness.StateRunning || state == harness.StateWaiting {
			t.Errorf("%s (%s) is still %s", it.ID, it.Kind, state)
		}
	}
	if st.Mission.Summary.Working {
		t.Error("the summary still says working")
	}
}

// apiAtLimit: a session over the engine's item log size keeps the newest
// items, says it dropped the older ones, and pages the same at any size.
func apiAtLimit(t *testing.T, agent string) {
	turns := make([]sessiongen.Turn, 20)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 30, Tools: 2, Output: 40}
	}
	s := apiStart(t, agent, 53, func(o *enginetest.Options) { o.MaxItems = 60 }, turns...)
	st := settleUntil(s.r, len(turns))
	if st.Discarded == 0 || len(st.Items) > 60 {
		t.Errorf("%d items kept, discarded through %d", len(st.Items), st.Discarded)
	}
	checkEdge(t, "at the limit", st, s.l)
	fail(t, "paging", enginetest.SamePaging(st, s.r.ReadPages(7)))
	fail(t, "paging", enginetest.Paging(s.r.ReadPages(3)))
}
