package hookstyle

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// agentOps turns a subagent hook into an agent row. Rows are keyed by the
// hook's agent id; Copilot names only the agent type, so a second agent of
// a running type gets its own row and a stop closes the oldest of its type.
func (m *Mapper) agentOps(p payload, at time.Time) []harness.Op {
	if p.AgentID == "" {
		return nil
	}
	row := harness.Row{ID: p.AgentID, Title: firstOf(p.AgentType, "Subagent"), State: harness.StateRunning, Started: &at}
	open := m.agents[p.AgentID]
	if p.Event == SubagentStop {
		if len(open) > 0 {
			row.ID, m.agents[p.AgentID] = open[0].id, open[1:]
			row.Title = open[0].title
		}
		row.State, row.Started = harness.StateOK, nil
	} else {
		if len(open) > 0 {
			row.ID = freeAgentID(p.AgentID, open)
		}
		m.agents[p.AgentID] = append(open, agentRow{row.ID, row.Title})
	}
	return []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}}
}

type agentRow struct{ id, title string }

// freeAgentID names one more agent of a type that already has open rows:
// the first "type#n" no open row holds. Counting the open rows is not
// enough, since an earlier one may have stopped while a later one runs.
func freeAgentID(base string, open []agentRow) string {
	for n := 2; ; n++ {
		id := base + "#" + strconv.Itoa(n)
		if !slices.ContainsFunc(open, func(a agentRow) bool { return a.id == id }) {
			return id
		}
	}
}

// closeAgents ends the agent rows no SubagentStop closed: the turn that
// started them is over.
func (m *Mapper) closeAgents(state string) []harness.Op {
	var ops []harness.Op
	for key, open := range m.agents {
		for _, a := range open {
			ops = append(ops, harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents,
				Row: &harness.Row{ID: a.id, Title: a.title, State: state}})
		}
		delete(m.agents, key)
	}
	slices.SortFunc(ops, func(a, b harness.Op) int { return strings.Compare(a.Row.ID, b.Row.ID) })
	return ops
}

// isPlanTool is a todo or plan tool by name.
func isPlanTool(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "todo") || low == "update_plan"
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
