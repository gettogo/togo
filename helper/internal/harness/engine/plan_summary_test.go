package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The plan summary counts tasks, not the phases that group them: an OMP
// plan of 7 phases and 20 tasks reads 20, and a phase row never counts
// as a task of its own.
func TestPlanSummaryCountsLeafTasks(t *testing.T) {
	m := newMission([]string{harness.CapChat, harness.CapPlan})
	leaf := func(id, state string) harness.Row { return harness.Row{ID: id, Title: id, State: state} }
	phase := func(id string, kids ...harness.Row) harness.Row {
		return harness.Row{ID: id, Title: id, State: harness.RowActive, Children: kids}
	}
	m.apply([]harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: []harness.Row{
		phase("p1", leaf("a", harness.RowDone), leaf("b", harness.RowSkipped), leaf("c", harness.RowPending)),
		phase("p2", leaf("d", harness.RowDone), leaf("e", harness.RowActive)),
		leaf("loose", harness.RowPending),
	}}})
	if p := m.summary.Plan; p == nil || p.Total != 6 || p.Done != 3 {
		t.Fatalf("plan = %+v, want 3/6", p)
	}
}
