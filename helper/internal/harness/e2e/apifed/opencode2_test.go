package apifed

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestOpenCode2LongSession: a long 2.x session of turns with reasoning,
// shell calls, child sessions and background shells, written in pieces
// while the helper reads it. Every check of the engine's rules holds
// after each piece; the children end as finished agents, the shells are
// background rows while they run and leave when they end.
func TestOpenCode2LongSession(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/scale/long_session", "opencode2/capability/chat", "opencode2/capability/chat.stream",
		"opencode2/capability/agents", "opencode2/capability/background", "opencode2/lifecycle/turn_end")
	n := support.Scale(30, 300)
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 40 + i%5*30, Tools: 1 + i%2, Output: 60}
		if i%4 == 3 {
			turns[i].Agents = &sessiongen.Batch{Size: 2, Steps: 1}
		}
		if i%5 == 2 {
			turns[i].Jobs = 1
		}
	}
	turns[n-1].Jobs = 2 // shells still running when the session goes idle
	s := oc2Start(t, 51, nil, turns[:4]...)
	st := support.SettleUntil(s.r, 4)
	support.CheckEdge(t, "start", st, s.l)
	for done := 4; done < n; {
		next := min(done+support.Scale(4, 40), n)
		s.add(t, turns[done:next]...)
		done = next
		st = support.SettleUntil(s.r, done)
		support.CheckEdge(t, "live", st, s.l)
	}
	running := s.p.Running()
	st = support.WaitFor(t, s.r, "the running shells as background rows", func(st enginetest.State) bool {
		return len(rows(st, harness.SectionBackground)) == len(running) && !st.Mission.Summary.Working
	})
	if len(running) == 0 {
		t.Fatal("the script left no shell running")
	}
	s.do(t, func() error { return s.p.EndShells(s.l) })
	st = support.WaitFor(t, s.r, "the background rows to leave", func(st enginetest.State) bool { return len(rows(st, harness.SectionBackground)) == 0 })
	if got := len(support.UserPrompts(st)); got != n {
		t.Errorf("%d prompts, want %d", got, n)
	}
	agents := map[string]bool{}
	for _, it := range support.Items(st, support.OfKind(harness.KindAgent)) {
		agents[it.Agent.AgentID] = true
		if it.Agent.State != harness.StateOK {
			t.Errorf("subagent %s is %s", it.ID, it.Agent.State)
		}
	}
	if len(agents) != len(s.l.Truth.Agents) {
		t.Errorf("%d subagents shown, %d ran", len(agents), len(s.l.Truth.Agents))
	}
	support.NothingRuns(t, st)
	if len(st.Pages) < 3 {
		t.Errorf("%d history pages, want the session to cross several", len(st.Pages))
	}
	for _, size := range []int{7, 50} {
		support.Fail(t, "paging", enginetest.SamePaging(st, s.r.ReadPages(size)))
	}
}

// rows lists a mission section's rows.
func rows(st enginetest.State, kind string) []harness.Row {
	var out []harness.Row
	for _, sec := range st.Mission.Sections {
		if sec.Kind == kind {
			out = append(out, sec.Rows...)
		}
	}
	return out
}

// TestOpenCode2AtLimit: a session over the engine's item log size keeps
// the newest items, says it dropped the older ones, and pages the same at
// any size, live and after a restart that reads the past again.
func TestOpenCode2AtLimit(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/scale/at_limit")
	turns := make([]sessiongen.Turn, 20)
	for i := range turns {
		turns[i] = sessiongen.Turn{Say: 30, Tools: 2, Output: 40}
	}
	s := oc2Start(t, 53, func(o *enginetest.Options) { o.MaxItems = 60 }, turns...)
	for _, where := range []string{"live", "restarted"} {
		if where == "restarted" {
			s.r.Restart()
		}
		st := support.SettleUntil(s.r, len(turns))
		if st.Discarded == 0 || len(st.Items) > 60 {
			t.Errorf("%s: %d items kept, discarded through %d", where, len(st.Items), st.Discarded)
		}
		support.CheckEdge(t, where, st, s.l)
		support.Fail(t, where+" paging", enginetest.SamePaging(st, s.r.ReadPages(7)))
		support.Fail(t, where+" paging", enginetest.Paging(s.r.ReadPages(3)))
	}
}
