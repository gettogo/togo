package claude

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// agentItem returns the agent item for an agent id, creating it from the
// Agent call that started it when known.
func (t *transcript) agentItem(agentID, callID string, at time.Time) *harness.Item {
	if it, ok := t.agents[agentID]; ok {
		return it
	}
	it := &harness.Item{ID: agentItemID(agentID), Kind: harness.KindAgent, At: at,
		Agent: &harness.AgentBody{AgentID: agentID, Name: "Agent", State: harness.StateRunning, Started: at}}
	if call, ok := t.tools[callID]; ok {
		it.TurnID, it.At, it.Agent.Started = call.TurnID, call.At, call.At
		it.Agent.Name = call.Tool.Title
		it.Agent.Role = t.roles[callID]
	}
	t.agents[agentID] = it
	return it
}

// linkAgent ties a subagent's first prompt to the Agent call that sent it.
func (t *transcript) linkAgent(agentID, prompt string, at time.Time) ([]harness.Item, []harness.Op) {
	if _, ok := t.agents[agentID]; ok {
		return nil, nil
	}
	call := t.prompts[strings.TrimSpace(prompt)]
	delete(t.prompts, strings.TrimSpace(prompt))
	it := t.agentItem(agentID, call, at)
	return t.putAgent(it, *it.Agent)
}

// agentResult reads an Agent call's result: the agent id, and whether it
// finished or runs on in the background.
func (t *transcript) agentResult(call *harness.Item, res map[string]any, state string, at time.Time) ([]harness.Item, []harness.Op) {
	if res == nil {
		return nil, nil
	}
	agentID := str(res, "agentId")
	if agentID == "" {
		return nil, nil
	}
	it := t.agentItem(agentID, call.ID, at)
	ag := *it.Agent
	switch {
	case str(res, "status") == "async_launched":
		ag.State = harness.StateRunning
	case state == harness.StateOK:
		ag.State = harness.StateOK
		end := at
		ag.Ended = &end
	default:
		ag.State = harness.StateFailed
		end := at
		ag.Ended = &end
	}
	if d := str(res, "description"); d != "" {
		ag.Name = d
	}
	return t.putAgent(it, ag)
}

func (t *transcript) putAgent(it *harness.Item, ag harness.AgentBody) ([]harness.Item, []harness.Op) {
	next := *it
	next.Agent = &ag
	t.agents[ag.AgentID] = &next
	started := ag.Started
	row := harness.Row{ID: next.ID, Title: ag.Name, Subtitle: ag.Role, State: ag.State, Started: &started,
		Open: &harness.Open{Kind: harness.OpenChat, Ref: next.ID}}
	return []harness.Item{next}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &row}}
}

// background announces a background shell a Bash call started.
func (t *transcript) background(taskID string, call *harness.Item, at time.Time) ([]harness.Item, []harness.Op) {
	t.bgTasks[taskID] = call.ID
	cmd := ""
	if call.Tool.Subject != nil {
		cmd = call.Tool.Subject.Text
	}
	it := harness.Item{ID: "bg-" + taskID, TurnID: call.TurnID, Kind: harness.KindBackground, At: at,
		Background: &harness.BackgroundBody{JobID: taskID, Kind: harness.BackgroundShell, Title: call.Tool.Title, Command: cmd,
			State: harness.StateRunning, Started: at}}
	t.tools["bg-"+taskID] = &it
	return []harness.Item{it}, []harness.Op{bgRow(it)}
}

func bgRow(it harness.Item) harness.Op {
	b := it.Background
	started := b.Started
	row := harness.Row{ID: b.JobID, Title: b.Title, Subtitle: b.Command, State: b.State, Started: &started}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &row}
}

var (
	taskIDTag  = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)
	statusTag  = regexp.MustCompile(`<status>([^<]+)</status>`)
	summaryTag = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)
)

// queued reads task notifications Claude Code queues for itself when a
// background shell or agent finishes.
func (t *transcript) queued(e entry) ([]harness.Item, []harness.Op) {
	if e.Operation != "enqueue" {
		return nil, nil
	}
	var content string
	if json.Unmarshal(e.Content, &content) != nil || !strings.Contains(content, "<task-notification>") {
		return nil, nil
	}
	m := taskIDTag.FindStringSubmatch(content)
	if m == nil {
		return nil, nil
	}
	id := strings.TrimSpace(m[1])
	status := ""
	if s := statusTag.FindStringSubmatch(content); s != nil {
		status = strings.TrimSpace(s[1])
	}
	state := harness.StateOK
	switch status {
	case "failed", "error":
		state = harness.StateFailed
	case "killed", "stopped", "cancelled":
		state = harness.StateCancelled
	case "running":
		state = harness.StateRunning
	}
	at := stamp(e.Timestamp)
	if ag, ok := t.agents[id]; ok {
		body := *ag.Agent
		body.State = state
		if state != harness.StateRunning {
			end := at
			body.Ended = &end
		}
		if s := summaryTag.FindStringSubmatch(content); s != nil {
			body.Summary = strings.TrimSpace(s[1])
		}
		return t.putAgent(ag, body)
	}
	if bg, ok := t.tools["bg-"+id]; ok {
		next := *bg
		body := *bg.Background
		body.State = state
		if state != harness.StateRunning {
			end := at
			body.Ended = &end
		}
		next.Background = &body
		t.tools["bg-"+id] = &next
		return []harness.Item{next}, []harness.Op{bgRow(next)}
	}
	return nil, nil
}
