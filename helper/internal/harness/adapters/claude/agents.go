package claude

import (
	"encoding/json"
	"fmt"
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
	if it.TurnID == "" && t.turn != nil {
		// The Agent call is not known (its prompt did not match, or it
		// came from before this mapper started): the agent belongs to the
		// turn that ran last, never to none, which drew a second working
		// line beside the turn's.
		it.TurnID = t.turn.id
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
	// The call's own description names the job; the command stays the subtitle.
	title := firstNonEmpty(t.shellAbout[call.ID], call.Tool.Title)
	it := harness.Item{ID: "bg-" + taskID, TurnID: call.TurnID, Kind: harness.KindBackground, At: at,
		Background: &harness.BackgroundBody{JobID: taskID, Kind: harness.BackgroundShell, Title: title, Command: cmd,
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

// isTaskNotification reports whether a main-chain user message is a task
// notification Claude Code sent itself rather than a person's prompt.
func isTaskNotification(e entry, text string) bool {
	var origin struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(e.Origin, &origin) == nil && origin.Kind == "task-notification" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(text), "<task-notification>")
}

// continued starts the turn Claude Code takes on its own after a task
// notification, opened by a quiet marker naming the trigger. One that
// arrives while a turn still runs belongs to that turn and draws nothing.
func (t *transcript) continued(e entry, at time.Time, content string) []harness.Item {
	if t.turn != nil && !t.turn.ended {
		return nil
	}
	id := e.UUID
	if id == "" {
		t.notices++
		id = fmt.Sprintf("continued-%d-%d", at.UnixMilli(), t.notices)
	}
	t.turn = &turnState{id: id, started: at, files: map[string]bool{}, state: harness.StateRunning}
	t.workingAt = at
	return []harness.Item{{ID: id, TurnID: id, Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeContinued, Text: t.trigger(content)}}}
}

var exitCodeTag = regexp.MustCompile(`\(exit code (-?\d+)\)`)

// trigger names what a task notification reports: a subagent or a
// background shell that finished, failed or was stopped.
func (t *transcript) trigger(content string) string {
	var id, status, summary string
	if m := taskIDTag.FindStringSubmatch(content); m != nil {
		id = strings.TrimSpace(m[1])
	}
	if m := statusTag.FindStringSubmatch(content); m != nil {
		status = strings.TrimSpace(m[1])
	}
	if m := summaryTag.FindStringSubmatch(content); m != nil {
		summary = strings.TrimSpace(m[1])
	}
	what := "Task"
	_, agent := t.agents[id]
	_, shell := t.tools["bg-"+id]
	switch {
	case agent || strings.HasPrefix(summary, "Agent "):
		what = "Subagent"
	case shell || strings.HasPrefix(summary, "Background command"):
		what = "Background task"
	}
	if m := exitCodeTag.FindStringSubmatch(summary); m != nil && m[1] != "0" && status == "completed" {
		status = "failed"
	}
	switch status {
	case "failed", "error":
		return what + " failed"
	case "killed", "stopped", "cancelled":
		return what + " stopped"
	}
	return what + " finished"
}

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
