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

// question shows AskUserQuestion prompts; Kimi takes the answer in its
// TUI, so the item is read-only and the Needs row opens the terminal.
func (w *Wire) question(r record, at time.Time) ([]harness.Item, []harness.Op) {
	if r.ID == "" {
		return nil, nil
	}
	var req struct {
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	json.Unmarshal(r.Request, &req)
	var qs []harness.Question
	for i, q := range req.Questions {
		hq := harness.Question{ID: strconv.Itoa(i), Header: q.Header, Prompt: q.Question, Multi: q.MultiSelect, AllowText: true}
		for _, o := range q.Options {
			hq.Options = append(hq.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
		}
		qs = append(qs, hq)
	}
	title := "Kimi is waiting for you"
	if len(qs) > 0 {
		title = x.Title(qs[0].Prompt)
	}
	if len(qs) == 0 {
		qs = []harness.Question{{ID: "0", Prompt: title}}
	}
	answerable := false // answered in Kimi's TUI only
	it := harness.Item{ID: r.ID, TurnID: w.turnID(), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs, Answerable: &answerable}}
	w.open[r.ID] = it
	return []harness.Item{it}, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionNeeds, Row: &harness.Row{ID: r.ID,
		Title: title, State: harness.RowActive, Started: x.Ptr(at), Open: &harness.Open{Kind: harness.OpenTerminal, Ref: w.env.SessionID}}}}
}

func (w *Wire) resolved(r record) ([]harness.Item, []harness.Op) {
	it, ok := w.open[r.ID]
	if !ok {
		return nil, nil
	}
	delete(w.open, r.ID)
	var resp struct {
		Answers map[string]string `json:"answers"`
	}
	json.Unmarshal(r.Response, &resp)
	qb := *it.Question
	qb.Answered = map[string][]string{}
	// Every question gets an entry, empty when the answer names none of
	// them (dismissed, or a key shape Kimi changed), so the card closes.
	for _, q := range qb.Questions {
		qb.Answered[q.ID] = []string{}
		if a, ok := resp.Answers[q.Prompt]; ok && a != "" {
			qb.Answered[q.ID] = []string{a}
		}
	}
	it.Question = &qb
	return []harness.Item{it}, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionNeeds, ID: r.ID}}
}
