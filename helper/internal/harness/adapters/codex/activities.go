package codex

import (
	"path"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Newer Codex versions report worker lifecycle directly. A wait call can
// have no receivers, so it cannot substitute for these activity records.
func (a *AppServer) activity(it threadItem, parent string, started, ended time.Time) ([]harness.Item, []harness.Op) {
	if it.AgentThreadID == "" || it.ID == "" {
		return nil, nil
	}
	switch it.ActivityKind {
	case "started", "interacted", "interrupted", "completed":
	default:
		return nil, nil
	}
	t := a.threads[it.AgentThreadID]
	if t != nil && t.activities[it.ActivityKind] == it.ID {
		return nil, nil // item/started and item/completed describe the same activity
	}
	known := t != nil && t.agent.Agent != nil
	x := a.agentItem(it.AgentThreadID, a.parentOf(parent), started)
	t = a.threads[it.AgentThreadID]
	if !known {
		name := path.Base(strings.TrimSpace(it.AgentPath))
		if name != "." && name != "/" && name != "" {
			x.Agent.Name = truncate(name, harness.MaxTitle)
		}
	}
	switch it.ActivityKind {
	case "started":
		if x.Agent.Ended == nil {
			x.Agent.State = harness.StateRunning
		}
	case "interacted":
		x.Agent.State, x.Agent.Ended = harness.StateRunning, nil
	case "interrupted":
		if x.Agent.Ended == nil {
			x.Agent.State, x.Agent.Ended = harness.StateInterrupted, ptr(ended)
		}
	case "completed":
		if x.Agent.Ended == nil {
			x.Agent.State, x.Agent.Ended = harness.StateOK, ptr(ended)
		}
	}
	if t.activities == nil {
		t.activities = map[string]string{}
	}
	t.activities[it.ActivityKind] = it.ID
	t.agent = x
	return []harness.Item{x}, []harness.Op{agentRow(x)}
}
