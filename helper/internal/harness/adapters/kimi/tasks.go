package kimi

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// taskRow is the mission row of an agent or background item.
func taskRow(it harness.Item) harness.Op {
	if it.Agent != nil {
		a := it.Agent
		return harness.Op{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &harness.Row{ID: a.AgentID, Title: a.Name,
			Subtitle: a.Role, State: rowState(a.State), Started: x.Ptr(a.Started), Open: &harness.Open{Kind: harness.OpenChat, Ref: it.ID}}}
	}
	b := it.Background
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &harness.Row{ID: b.JobID, Title: b.Title,
		Subtitle: b.Command, State: rowState(b.State), Started: x.Ptr(b.Started)}}
}

func rowState(s string) string {
	switch s {
	case harness.StateRunning, harness.StateWaiting:
		return harness.RowActive
	case harness.StateOK:
		return harness.RowDone
	}
	return harness.RowSkipped
}

// taskStarted is a detached process Kimi started outside a tool result.
func (w *Wire) taskStarted(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var in struct {
		TaskID      string `json:"taskId"`
		Description string `json:"description"`
		Command     string `json:"command"`
		StartedAt   int64  `json:"startedAt"`
	}
	if json.Unmarshal(raw, &in) != nil || in.TaskID == "" {
		return nil, nil
	}
	if _, ok := w.tasks[in.TaskID]; ok {
		return nil, nil
	}
	started := x.Or(x.Millis(in.StartedAt), at)
	bg := harness.Item{ID: "bg-" + in.TaskID, TurnID: w.turnID(), Kind: harness.KindBackground, At: started,
		Background: &harness.BackgroundBody{JobID: in.TaskID, Kind: harness.BackgroundShell, Title: x.Title(firstNonEmpty(in.Description, in.Command)),
			Command: x.Truncate(in.Command, harness.MaxTitle), State: harness.StateRunning, Started: started}}
	w.tasks[in.TaskID] = bg
	return []harness.Item{bg}, []harness.Op{taskRow(bg)}
}

// taskDone closes an agent or background task from its notification.
func (w *Wire) taskDone(id, status string, at time.Time) ([]harness.Item, []harness.Op) {
	it, ok := w.tasks[id]
	if !ok {
		return nil, nil
	}
	delete(w.tasks, id)
	state := harness.StateOK
	switch status {
	case "completed":
	case "killed", "cancelled":
		state = harness.StateCancelled
	default:
		state = harness.StateFailed
	}
	if it.Agent != nil {
		a := *it.Agent
		a.State, a.Ended = state, x.Ptr(at)
		it.Agent = &a
	} else {
		b := *it.Background
		b.State, b.Ended = state, x.Ptr(at)
		it.Background = &b
	}
	return []harness.Item{it}, []harness.Op{taskRow(it)}
}

// plan turns a TodoList call into the plan section and a plan marker.
func (w *Wire) plan(raw json.RawMessage, at time.Time) ([]harness.Item, []harness.Op) {
	var a struct {
		Todos []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(raw, &a) != nil || len(a.Todos) == 0 {
		var probe map[string]json.RawMessage
		if json.Unmarshal(raw, &probe) != nil || probe["todos"] == nil {
			w.NoteUnrecognisedPlan() // the call stays a tool step
		}
		return nil, nil
	}
	rows := make([]harness.Row, 0, len(a.Todos))
	body := harness.PlanBody{Total: len(a.Todos)}
	for i, t := range a.Todos {
		st := harness.RowPending
		switch t.Status {
		case "in_progress":
			st = harness.RowActive
			if body.Current == "" {
				body.Current = x.Title(t.Title)
			}
		case "done", "completed":
			st = harness.RowDone
			body.Done++
		case "cancelled", "skipped":
			st = harness.RowSkipped
		}
		rows = append(rows, harness.Row{ID: strconv.Itoa(i), Title: x.Title(t.Title), State: st})
	}
	it := harness.Item{ID: "plan-" + strconv.FormatInt(at.UnixMilli(), 36), TurnID: w.turnID(), Kind: harness.KindPlan, At: at, Plan: &body}
	return []harness.Item{it}, []harness.Op{{Op: harness.OpReplace, Section: harness.SectionPlan, Rows: rows}}
}
