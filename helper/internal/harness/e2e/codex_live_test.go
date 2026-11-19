package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// A turn as it runs: the reply streams in, the plan and the changed file
// show, and the status block carries the thread's model, effort and
// approval policy, the context the last request filled and the account's
// rate limits, from the app-server's own notifications.
func TestCodexLiveTurn(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/capability/chat.stream", "codex/capability/plan",
		"codex/capability/status", "codex/capability/status.usage", "codex/capability/status.context")
	c := codexStart(t, 71, nil, sessiongen.Turn{Say: 20})
	settleUntil(c.r, 1)
	c.joined(t)
	c.p.Plan = true
	release := c.hold(t, sessiongen.Turn{Say: 60, Tools: 1, Output: 40}, method("item/agentMessage/delta"))
	st := c.r.SettleUntil(quick, func(st enginetest.State) bool {
		for _, it := range items(st, ofKind(harness.KindAssistant)) {
			if it.Assistant.Partial {
				return true
			}
		}
		return false
	})
	if !st.Mission.Summary.Working {
		t.Error("the turn runs and the summary is not working")
	}
	var partial harness.Item
	for _, it := range items(st, ofKind(harness.KindAssistant)) {
		if it.Assistant.Partial {
			partial = it
		}
	}
	plan := 0
	for _, sec := range st.Mission.Sections {
		if sec.Kind == harness.SectionPlan {
			plan += len(sec.Rows)
		}
	}
	if plan != 3 {
		t.Errorf("%d plan rows while the turn runs, want 3", plan)
	}
	release()
	st = c.r.SettleUntil(quick, func(st enginetest.State) bool { return hasPrompt(2)(st) && !st.Mission.Summary.Working })
	final, ok := latestOf(st, partial.ID)
	if !ok || final.Assistant.Partial || !strings.HasPrefix(final.Assistant.Text, partial.Assistant.Text) || len(final.Assistant.Text) <= len(partial.Assistant.Text) {
		t.Errorf("streamed %q, then %+v", partial.Assistant.Text, final.Assistant)
	}
	s := statusOf(st)
	if s.Model != sessiongen.CodexModel || s.Effort != sessiongen.CodexEffort || s.Mode != sessiongen.CodexPolicy {
		t.Errorf("status %+v, want the thread's model, effort and approval policy", s)
	}
	if s.ContextPercent == nil || *s.ContextPercent <= 0 {
		t.Errorf("no context use: %+v", s.ContextPercent)
	}
	labels := []string{}
	for _, m := range s.Meters {
		labels = append(labels, m.Label)
		if m.Resets == nil {
			t.Errorf("meter %s has no reset time", m.Label)
		}
	}
	if strings.Join(labels, ",") != "5 hour limit,Weekly limit" {
		t.Errorf("meters %q", labels)
	}
	if changes := sectionRows(st, harness.SectionChanges); changes == 0 {
		t.Error("the changed file is not shown")
	}
	nothingRuns(t, st)
}

// TestKnownFault_Codex_RestartLosesPlanAndChanges: after a helper
// restart the plan and the changed files of the thread are gone. Both
// reach the helper only as notifications, turn/plan/updated and
// turn/diff/updated (adapters/codex/live.go:95, 131); the thread/resume a
// restarted helper joins with carries turns and items, and the history
// replay draws neither from them (appserver.go:259: file change items
// become tool rows, not changes), and the helper keeps neither under
// TOGO_HOME/run. A helper that joins a thread after its plan or diff was
// sent (a reconnect, a Codex restart) shows neither, the same way.
//
// Correct: the plan and the changes shown before the restart are shown
// after it.
func TestKnownFault_Codex_RestartLosesPlanAndChanges(t *testing.T) {
	t.Parallel()
	c := codexStart(t, 73, nil)
	c.joined(t)
	c.p.Plan = true
	c.add(t, sessiongen.Turn{Say: 20, Tools: 1, Output: 20})
	before := c.r.SettleUntil(quick, func(st enginetest.State) bool {
		return hasPrompt(1)(st) && !st.Mission.Summary.Working
	})
	if sectionRows(before, harness.SectionPlan) != 3 || sectionRows(before, harness.SectionChanges) != 1 {
		t.Fatalf("before the restart: %d plan rows, %d changed files", sectionRows(before, harness.SectionPlan), sectionRows(before, harness.SectionChanges))
	}
	c.r.Restart()
	after := settleUntil(c.r, 1)
	if p, ch := sectionRows(after, harness.SectionPlan), sectionRows(after, harness.SectionChanges); p != 0 || ch != 0 {
		t.Fatalf("after the restart: %d plan rows, %d changed files: the fault changed or is fixed; check plans and changes across the restarts of TestCodexLongSession", p, ch)
	}
}

// joined waits until the helper has joined the terminal's thread, so what
// is written next reaches it as it happens.
func (c *codexSession) joined(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(quick); len(c.s.CallsOf("thread/resume")) == 0 || c.s.Conns() == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the helper never joined the thread")
		}
	}
	c.r.Settle(time.Second)
}

func sectionRows(st enginetest.State, kind string) int {
	n := 0
	for _, sec := range st.Mission.Sections {
		if sec.Kind == kind {
			n += len(sec.Rows)
		}
	}
	return n
}

func latestOf(st enginetest.State, id string) (harness.Item, bool) {
	for _, it := range items(st, func(it harness.Item) bool { return it.ID == id }) {
		return it, true
	}
	return harness.Item{}, false
}
