package hookstyle

import (
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// agentOps turns a subagent hook into an agent row.
func agentOps(p payload, at time.Time) []harness.Op {
	if p.AgentID == "" {
		return nil
	}
	row := harness.Row{ID: p.AgentID, Title: firstOf(p.AgentType, "Subagent"), State: harness.RowActive, Started: &at}
	if p.Event == SubagentStop {
		row.State, row.Started = harness.RowDone, nil
	}
	return []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}}
}

// planOps turns a task hook into a plan row.
func planOps(p payload) []harness.Op {
	id := p.TaskID
	if id == "" {
		return nil
	}
	title := Truncate(firstOf(p.TaskSubject, p.TaskDesc, "Task "+id), harness.MaxTitle)
	state := harness.RowPending
	if p.Event == TaskCompleted {
		state = harness.RowDone
	}
	return []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionPlan, Row: &harness.Row{ID: id, Title: title, State: state}}}
}

// modelName reads a model given as a string or as {id, display_name}.
func modelName(rawModel json.RawMessage) string {
	var s string
	if json.Unmarshal(rawModel, &s) == nil {
		return s
	}
	var o struct {
		ID   string `json:"id"`
		Name string `json:"display_name"`
	}
	if json.Unmarshal(rawModel, &o) == nil {
		return firstOf(o.ID, o.Name)
	}
	return ""
}

// raw is a JSON string's value, or "" for any other value.
func raw(v json.RawMessage) string {
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}
