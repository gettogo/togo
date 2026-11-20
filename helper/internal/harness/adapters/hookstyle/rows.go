package hookstyle

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// agentOps turns a subagent hook into an agent item in the turn's chat
// and its row. Rows are keyed by the hook's agent id; older Copilot
// payloads name only the agent type, so a second agent of a running type
// gets its own row and a stop closes the oldest of its type.
func (m *Mapper) agentOps(p payload, at time.Time) ([]harness.Item, []harness.Op) {
	if p.AgentID == "" {
		return nil, nil
	}
	open := m.agents[p.AgentID]
	var a agentRow
	if p.Event == SubagentStop {
		if len(open) == 0 {
			// Never announced here (or already closed with its turn):
			// a row of its own, finished.
			a = m.newAgent(p.AgentID, p.AgentType, at)
		} else {
			a, m.agents[p.AgentID] = open[0], open[1:]
			if len(m.agents[p.AgentID]) == 0 {
				delete(m.agents, p.AgentID)
			}
		}
		m.subs[p.AgentID] = true
		b := *a.item.Agent
		b.State, b.Ended, b.Summary = harness.StateOK, &at, Clip(firstOf(p.Response, p.LastMessage))
		a.item.Agent = &b
		return []harness.Item{a.item}, []harness.Op{a.op()}
	}
	id := p.AgentID
	if len(open) > 0 {
		id = freeAgentID(p.AgentID, open)
	}
	a = m.newAgent(id, p.AgentType, at)
	if up := m.agents[p.SessionID]; p.SessionID != p.AgentID && len(up) > 0 && m.fromSub(p) {
		// A subagent's own subagent: drawn in its conversation.
		a.item.Parent = up[0].item.ID
	}
	m.agents[p.AgentID] = append(open, a)
	if p.AgentID != p.SessionID {
		m.noteSub(p.AgentID)
	}
	return []harness.Item{a.item}, []harness.Op{a.op()}
}

// agentRow is one subagent: its row id and the chat item that draws it.
type agentRow struct {
	id   string
	item harness.Item
}

// newAgent builds a running subagent's item in the turn that runs, named
// after the task call that started it when one runs.
func (m *Mapper) newAgent(id, kind string, at time.Time) agentRow {
	name := firstOf(kind, "Subagent")
	if call, ok := m.taskCall(); ok {
		name = call.Tool.Title
	}
	return agentRow{id: id, item: harness.Item{ID: "agent-" + id, TurnID: m.turn, Kind: harness.KindAgent, At: at,
		Agent: &harness.AgentBody{AgentID: id, Name: Truncate(name, harness.MaxTitle), Role: kind, State: harness.StateRunning, Started: at}}}
}

func (a agentRow) op() harness.Op {
	b := a.item.Agent
	row := harness.Row{ID: a.id, Title: firstOf(b.Role, b.Name), Subtitle: subtitle(b), State: b.State,
		Open: &harness.Open{Kind: harness.OpenChat, Ref: a.item.ID}}
	if b.State == harness.StateRunning {
		started := b.Started
		row.Started = &started
	}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}
}

// subtitle is the task's own title under the agent type, when they differ.
func subtitle(b *harness.AgentBody) string {
	if b.Role != "" && b.Name != b.Role {
		return b.Name
	}
	return ""
}

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

// closeAgents ends the agents no SubagentStop closed: the turn that
// started them is over.
func (m *Mapper) closeAgents(state string, at time.Time) ([]harness.Item, []harness.Op) {
	var all []agentRow
	for key, open := range m.agents {
		all = append(all, open...)
		delete(m.agents, key)
		m.subs[key] = true
	}
	slices.SortFunc(all, func(a, b agentRow) int { return strings.Compare(a.id, b.id) })
	var items []harness.Item
	var ops []harness.Op
	for _, a := range all {
		b := *a.item.Agent
		b.State, b.Ended = state, &at
		a.item.Agent = &b
		items, ops = append(items, a.item), append(ops, a.op())
	}
	return items, ops
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
