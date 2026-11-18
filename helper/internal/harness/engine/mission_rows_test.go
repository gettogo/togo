package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func shownRows(m *missionState, section string) []string {
	ids := []string{}
	if sec := m.section(section); sec != nil {
		for _, r := range sec.Rows {
			ids = append(ids, r.ID+":"+r.State)
		}
	}
	return ids
}

func upsert(section, id, state string) harness.Op {
	return harness.Op{Op: harness.OpUpsert, Section: section, Row: &harness.Row{ID: id, Title: id, State: state}}
}

// Mission control lists what runs and what went wrong since the last
// prompt: a row that ended well leaves at once, one that failed or was
// stopped stays until the user's next prompt, and a state from a harness's
// own vocabulary is never taken for finished.
func TestAgentsAndShellsLeaveWhenTheyEndWell(t *testing.T) {
	for _, section := range []string{harness.SectionAgents, harness.SectionBackground} {
		m := newMission([]string{harness.CapAgents, harness.CapBackground, harness.CapPlan})
		m.apply([]harness.Op{upsert(section, "a", harness.StateRunning), upsert(section, "b", harness.StateRunning),
			upsert(section, "c", harness.StateWaiting), upsert(section, "d", "ready"), upsert(section, "e", "some-new-word")})
		if got := shownRows(m, section); len(got) != 5 {
			t.Fatalf("%s: live rows = %v, want all five", section, got)
		}
		d := m.apply([]harness.Op{upsert(section, "a", harness.StateOK)})
		if d == nil || len(d.Ops) == 0 || d.Ops[0].Op != harness.OpRemove || d.Ops[0].ID != "a" {
			t.Fatalf("%s: a clean end = %+v, want the row's removal", section, d)
		}
		m.apply([]harness.Op{upsert(section, "b", harness.StateFailed), upsert(section, "c", harness.StateInterrupted)})
		want := []string{"b:failed", "c:interrupted", "d:ready", "e:some-new-word"}
		if got := shownRows(m, section); !equalStrings(got, want) {
			t.Fatalf("%s: after a clean end and two failures = %v, want %v", section, got, want)
		}
		// A row that never showed and ends well changes nothing.
		if d := m.apply([]harness.Op{upsert(section, "z", harness.StateOK)}); d != nil {
			t.Fatalf("%s: an unseen row ending well made a delta: %+v", section, d)
		}
		// A failed agent that is run again is live again, and no longer leaves with the prompt.
		m.apply([]harness.Op{upsert(section, "c", harness.StateRunning)})
		d = m.prompted()
		if d == nil {
			t.Fatalf("%s: the next prompt removed nothing", section)
		}
		want = []string{"c:running", "d:ready", "e:some-new-word"}
		if got := shownRows(m, section); !equalStrings(got, want) {
			t.Fatalf("%s: after the next prompt = %v, want %v", section, got, want)
		}
		if d := m.prompted(); d != nil {
			t.Fatalf("%s: a second prompt removed more: %+v", section, d)
		}
	}
}

// A list sent whole (a source-driven section) follows the same rule, and
// the plan, which is not a list of live things, keeps its finished rows.
func TestReplacedListsAndOtherSectionsKeepTheirRules(t *testing.T) {
	m := newMission([]string{harness.CapAgents, harness.CapBackground, harness.CapPlan})
	m.apply([]harness.Op{{Op: harness.OpReplace, Section: harness.SectionBackground, Rows: []harness.Row{
		{ID: "s1", State: "exited"}, {ID: "s2", State: "ready"}, {ID: "s3", State: harness.StateRunning}, {ID: "s4", State: harness.StateFailed}}}})
	if got, want := shownRows(m, harness.SectionBackground), []string{"s2:ready", "s3:running", "s4:failed"}; !equalStrings(got, want) {
		t.Fatalf("replaced shells = %v, want %v", got, want)
	}
	m.prompted()
	if got, want := shownRows(m, harness.SectionBackground), []string{"s2:ready", "s3:running"}; !equalStrings(got, want) {
		t.Fatalf("shells after the next prompt = %v, want %v", got, want)
	}
	m.apply([]harness.Op{upsert(harness.SectionPlan, "p1", "done"), upsert(harness.SectionPlan, "p2", "active")})
	if got := shownRows(m, harness.SectionPlan); len(got) != 2 {
		t.Fatalf("plan rows = %v, want both kept", got)
	}
	if m.summary.Agents != 0 || m.summary.Background != 1 {
		t.Errorf("summary = %+v, want the one running shell", m.summary)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
