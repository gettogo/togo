package codex

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// agentItem returns a copy. Updating a cached agent must not mutate an
// earlier emitted item that the engine or a test still owns.
func (a *AppServer) agentItem(id, parent string, at time.Time) harness.Item {
	t := a.threads[id]
	if t == nil {
		t = &threadInfo{}
		a.threads[id] = t
	}
	t.parent = "agent-" + id
	if t.agent.Agent != nil {
		x := t.agent
		body := *x.Agent
		x.Agent = &body
		return x
	}
	return harness.Item{ID: t.parent, Parent: parent, Kind: harness.KindAgent, At: at,
		Agent: &harness.AgentBody{AgentID: id, Name: "Subagent", State: harness.StateRunning, Started: at}}
}

// resumedAgent enriches the identity already seen in a spawn. A reconnect
// or a repeated resume never substitutes its arrival time for the clock.
func (a *AppServer) resumedAgent(r resumeResult, at time.Time) harness.Item {
	id := r.Thread.ID
	known := a.threads[id] != nil && a.threads[id].agent.Agent != nil
	x := a.agentItem(id, a.parentOf(*r.Thread.ParentThreadID), at)
	if r.Thread.AgentNickname != nil && strings.TrimSpace(*r.Thread.AgentNickname) != "" {
		x.Agent.Name = truncate(*r.Thread.AgentNickname, harness.MaxTitle)
	} else if r.Thread.Name != nil && strings.TrimSpace(*r.Thread.Name) != "" {
		x.Agent.Name = truncate(*r.Thread.Name, harness.MaxTitle)
	}
	if r.Thread.AgentRole != nil && strings.TrimSpace(*r.Thread.AgentRole) != "" {
		x.Agent.Role = *r.Thread.AgentRole
	}
	started := secs(r.Thread.CreatedAt)
	if started.IsZero() && !known {
		for _, turn := range r.Thread.Turns {
			when := secs(turn.StartedAt)
			if !when.IsZero() && (started.IsZero() || when.Before(started)) {
				started = when
			}
		}
	}
	if !started.IsZero() && (!known || started.Before(x.Agent.Started)) {
		x.Agent.Started = started
		x.At = started
	}
	if r.Thread.Status.Type == "active" {
		x.Agent.State = harness.StateRunning
		x.Agent.Ended = nil
	} else if !known {
		x.Agent.State = harness.StateWaiting
	}
	a.threads[id].agent = x
	return x
}

// collab updates the stored identity; only first discovery can name an
// agent from a prompt. Follow-up instructions are never its name.
func (a *AppServer) collab(it threadItem, thread string, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	for _, id := range it.ReceiverThreadIDs {
		known := a.threads[id] != nil && a.threads[id].agent.Agent != nil
		x := a.agentItem(id, a.parentOf(thread), at)
		if !known && it.Prompt != nil {
			name := strings.TrimSpace(strings.SplitN(*it.Prompt, "\n", 2)[0])
			if name != "" {
				x.Agent.Name = truncate(name, harness.MaxTitle)
			}
		}
		if state, ok := it.AgentsStates[id]; ok {
			if next, valid := agentLifecycle(state.Status); valid {
				x.Agent.State = next
				if next == harness.StateRunning || next == harness.StateWaiting {
					x.Agent.Ended = nil
				} else if x.Agent.Ended == nil {
					x.Agent.Ended = ptr(at)
				}
			}
		}
		a.threads[id].agent = x
		items = append(items, x)
		ops = append(ops, agentRow(x))
	}
	return items, ops
}

func agentLifecycle(status string) (string, bool) {
	switch status {
	case "inProgress", "pendingInit", "running", "completed", "failed", "errored", "declined", "interrupted", "shutdown":
		return runState(status), true
	case "notFound":
		return harness.StateFailed, true
	}
	return "", false
}

func agentRow(x harness.Item) harness.Op {
	row := harness.Row{ID: x.Agent.AgentID, Title: x.Agent.Name, Subtitle: x.Agent.Role, State: x.Agent.State,
		Started: ptr(x.Agent.Started), Open: &harness.Open{Kind: harness.OpenChat, Ref: x.ID}}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}
}
