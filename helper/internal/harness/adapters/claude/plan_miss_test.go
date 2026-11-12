package claude

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func toolLine(id, name, input string) harness.Record {
	raw := `{"type":"assistant","message":{"model":"claude-sonnet-4-5","id":"m_` + id + `","role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}]},"uuid":"u_` + id + `","timestamp":"2026-11-03T13:00:06.000Z","sessionId":"s2"}`
	return harness.Record{Source: "transcript", At: time.Unix(0, 0), Data: json.RawMessage(raw)}
}

// A plan call in a shape this adapter does not read stays a tool step and
// is flagged for the log; a readable one still goes to the plan.
func TestUnrecognisedPlanCallStaysAToolStep(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, []harness.Record{toolLine("t1", "TodoWrite", `{"tasks":[{"title":"x"}]}`)})
	if it := r.final["t1"]; it.Tool == nil || it.Tool.Name != "TodoWrite" {
		t.Fatalf("no tool step: %+v", r.final)
	}
	if !m.PlanUnrecognised() {
		t.Fatal("not flagged")
	}

	m = newTranscript(env())
	r = mapAll(m, []harness.Record{toolLine("t2", "TodoWrite", `{"todos":[{"content":"x","status":"pending"}]}`)})
	if _, step := r.final["t2"]; step || m.PlanUnrecognised() {
		t.Fatalf("readable call: step=%v flagged=%v", step, m.PlanUnrecognised())
	}
	plan := false
	for _, op := range r.ops {
		plan = plan || (op.Section == harness.SectionPlan && len(op.Rows) == 1)
	}
	if !plan {
		t.Fatal("no plan rows")
	}
}
