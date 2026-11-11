package kimi

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	x "github.com/gettogo/togo/helper/internal/harness/adapters/internal/itemtext"
)

// Kimi Code (2.5) logs every prompt it raises as interaction.request {id,
// kind, toolCallId, request} and its answer as interaction.resolved {id,
// response}, whoever answered (read from 2.5.3's agent-core and real
// wire logs):
//
//   - kind question (AskUserQuestion): request.questions[]{question,
//     header, multiSelect, options[]{label, description}}; the TUI adds an
//     Other row that takes words. response.answers maps each question text
//     to the label picked, a multi-select's labels joined with ", ", or the
//     typed words; a dismissed question resolves with null.
//   - kind approval: request {toolName, action, display{kind, ...}}. The
//     TUI offers Approve once / Approve for this session / Reject / Reject
//     with feedback; response {decision: approved | rejected | cancelled,
//     scope: session, feedback}. ExitPlanMode's display is plan_review
//     {plan, path, options?}: Approve (or the plan's own options) / Reject /
//     Revise (with feedback), and the response names selectedLabel.
//
// Each becomes a question item (id the interaction id) answered with keys
// on the dialog; the plan is drawn as an assistant item first.

type interaction struct {
	ToolName string `json:"toolName"`
	Action   string `json:"action"`
	Display  struct {
		Kind    string `json:"kind"`
		Plan    string `json:"plan"`
		Options []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"display"`
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

// Kimi's own approval labels (tui reverse-rpc approval adapter).
const (
	approveOnce     = "Approve once"
	approveSession  = "Approve for this session"
	reject          = "Reject"
	rejectFeedback  = "Reject with feedback"
	planApprove     = "Approve"
	planRevise      = "Revise"
	planRejectLabel = "Reject"
)

// headerFor is the title the TUI draws over an approval.
func headerFor(tool string) string {
	switch tool {
	case "Bash":
		return "Run this command?"
	case "Write":
		return "Write this file?"
	case "Edit":
		return "Apply these edits?"
	case "TaskStop":
		return "Stop this task?"
	case "ExitPlanMode":
		return "Ready to build with this plan?"
	}
	return "Approve " + tool + "?"
}

func (w *Wire) interaction(r record, at time.Time) []harness.Item {
	if r.ID == "" {
		return nil
	}
	var req interaction
	json.Unmarshal(r.Request, &req)
	switch r.Kind {
	case "question":
		return []harness.Item{w.ask(r.ID, req, at)}
	case "approval":
		return w.approval(r, req, at)
	}
	return nil
}

func (w *Wire) ask(id string, req interaction, at time.Time) harness.Item {
	var qs []harness.Question
	for i, q := range req.Questions {
		hq := harness.Question{ID: strconv.Itoa(i), Header: q.Header, Prompt: q.Question, Multi: q.MultiSelect,
			AllowText: true, Options: []harness.Choice{}}
		for _, o := range q.Options {
			hq.Options = append(hq.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
		}
		qs = append(qs, hq)
	}
	if len(qs) == 0 {
		qs = []harness.Question{{ID: "0", Prompt: "Kimi is asking", Options: []harness.Choice{}, AllowText: true}}
	}
	it := harness.Item{ID: id, TurnID: w.turnID(), Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
	w.open[id] = it
	return it
}

func (w *Wire) approval(r record, req interaction, at time.Time) []harness.Item {
	var items []harness.Item
	q := harness.Question{ID: "0", Prompt: headerFor(req.ToolName), Header: x.Title(req.Action), AllowText: true}
	body := &harness.QuestionBody{}
	if req.Display.Kind == "plan_review" || req.ToolName == "ExitPlanMode" {
		if plan := strings.TrimSpace(req.Display.Plan); plan != "" {
			call := firstNonEmpty(r.ToolCallID, r.ID)
			items = append(items, harness.Item{ID: call + "-plan", TurnID: w.turnID(), Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: x.Clip(plan)}})
		}
		q.Header = ""
		if len(req.Display.Options) >= 2 {
			for _, o := range req.Display.Options {
				q.Options = append(q.Options, harness.Choice{ID: o.Label, Label: o.Label, Detail: o.Description})
			}
		} else {
			q.Options = []harness.Choice{{ID: planApprove, Label: planApprove}}
		}
		q.Options = append(q.Options, harness.Choice{ID: planRejectLabel, Label: planRejectLabel},
			harness.Choice{ID: planRevise, Label: planRevise, Detail: "Type what to change"})
		body.Approve, body.Deny = q.Options[0].ID, planRejectLabel
	} else {
		q.Options = []harness.Choice{{ID: approveOnce, Label: approveOnce}, {ID: approveSession, Label: approveSession},
			{ID: reject, Label: reject}, {ID: rejectFeedback, Label: rejectFeedback, Detail: "Type why"}}
		body.Approve, body.Deny = approveOnce, reject
	}
	body.Questions = []harness.Question{q}
	it := harness.Item{ID: r.ID, TurnID: w.turnID(), Kind: harness.KindQuestion, At: at, Question: body}
	w.open[r.ID] = it
	return append(items, it)
}

type resolution struct {
	Answers       map[string]string `json:"answers"`
	Decision      string            `json:"decision"`
	Scope         string            `json:"scope"`
	Feedback      string            `json:"feedback"`
	SelectedLabel string            `json:"selectedLabel"`
}

func (w *Wire) resolved(r record) []harness.Item {
	it, ok := w.open[r.ID]
	if !ok {
		return nil
	}
	delete(w.open, r.ID)
	var resp resolution
	json.Unmarshal(r.Response, &resp) // null when dismissed
	qb := *it.Question
	qb.Answered = map[string][]string{}
	// Every question gets an entry, empty when the answer names none of
	// them (dismissed, cancelled, a shape Kimi changed), so the card closes.
	for _, q := range qb.Questions {
		qb.Answered[q.ID] = []string{}
	}
	if qb.Approve != "" {
		if a := decided(resp, qb.Questions[0]); a != "" {
			qb.Answered["0"] = []string{a}
		}
	} else {
		for _, q := range qb.Questions {
			if a, ok := resp.Answers[q.Prompt]; ok && a != "" {
				qb.Answered[q.ID] = picked(q, a)
			}
		}
	}
	it.Question = &qb
	return []harness.Item{it}
}

// decided names the approval option chosen, or the typed feedback.
func decided(resp resolution, q harness.Question) string {
	if resp.Feedback != "" && resp.Decision == "rejected" {
		return resp.Feedback
	}
	if resp.SelectedLabel != "" {
		return resp.SelectedLabel
	}
	switch resp.Decision {
	case "approved":
		if resp.Scope == "session" {
			return approveSession
		}
		return q.Options[0].ID
	case "rejected":
		return reject
	}
	return ""
}

// picked splits a multi-select answer ("Auth, Search") into its labels;
// anything that is not wholly labels is the typed answer.
func picked(q harness.Question, a string) []string {
	has := func(l string) bool {
		for _, o := range q.Options {
			if o.Label == l {
				return true
			}
		}
		return false
	}
	if has(a) || !q.Multi {
		return []string{a}
	}
	parts := strings.Split(a, ", ")
	for _, p := range parts {
		if !has(p) {
			return []string{a}
		}
	}
	return parts
}

// closeOpen closes every prompt still waiting (the log restarted: Kimi
// cancels pending interactions with its process).
func (w *Wire) closeOpen() []harness.Item {
	var items []harness.Item
	for id := range w.open {
		items = append(items, w.resolved(record{ID: id})...)
	}
	return items
}
