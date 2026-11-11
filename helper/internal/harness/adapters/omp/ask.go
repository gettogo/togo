package omp

import (
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp's ask tool (18.16.0) asks the user one or more questions in its TUI:
// a toolCall block named "ask" whose arguments hold
//
//	questions: [{id, header?, question, multi?, options: [{label, description}], recommended?}]
//
// and, once answered, a toolResult whose details hold the answers:
// {question, options, multi, selectedOptions, customInput?} for one
// question, or results: [{id, question, options, multi, selectedOptions,
// customInput?}] for a batch. A cancelled dialog (Escape) is a toolResult
// with isError and no details. The dialog itself is driven from the phone
// with keys (the screen_answer builtin over detect.ParseChoice), so the
// option ids sent back are the labels the rows show, and every question
// takes typed words through its "Other (type your own)" row.

// askQuestion turns an ask call into a question item, or nil when the
// call asks nothing.
func askQuestion(callID, tid, parent string, at time.Time, args map[string]any) *harness.Item {
	var qs []harness.Question
	for i, el := range asList(args["questions"]) {
		m := obj(el)
		q := harness.Question{ID: firstNonEmpty(str(m, "id"), itoa(i)), Header: str(m, "header"), Prompt: str(m, "question"),
			Options: []harness.Choice{}, AllowText: true}
		q.Multi, _ = m["multi"].(bool)
		for _, o := range asList(m["options"]) {
			om := obj(o)
			label := str(om, "label")
			if label == "" {
				continue
			}
			q.Options = append(q.Options, harness.Choice{ID: label, Label: truncate(label, harness.MaxTitle), Detail: truncate(str(om, "description"), harness.MaxTitle)})
		}
		if q.Prompt == "" {
			continue
		}
		qs = append(qs, q)
	}
	if len(qs) == 0 {
		return nil
	}
	return &harness.Item{ID: "ask-" + callID, TurnID: tid, Parent: parent, Kind: harness.KindQuestion, At: at,
		Question: &harness.QuestionBody{Questions: qs}}
}

// askAnswers reads the ask result's details into the item's answers: the
// labels selected and the custom input, per question; every question gets
// an entry, empty when it was cancelled or skipped, so the item closes.
func askAnswers(q *harness.QuestionBody, m map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, qu := range q.Questions {
		out[qu.ID] = []string{}
	}
	fill := func(id string, r map[string]any) {
		vals := []string{}
		for _, s := range asList(r["selectedOptions"]) {
			if v, ok := s.(string); ok && v != "" {
				vals = append(vals, v)
			}
		}
		if c := str(r, "customInput"); c != "" {
			vals = append(vals, clip(c))
		}
		out[id] = vals
	}
	details := obj(m["details"])
	if results := asList(details["results"]); len(results) > 0 {
		for i, r := range results {
			rm := obj(r)
			id := str(rm, "id")
			if _, ok := out[id]; !ok && i < len(q.Questions) {
				id = q.Questions[i].ID
			}
			if _, ok := out[id]; ok {
				fill(id, rm)
			}
		}
		return out
	}
	if len(q.Questions) == 1 && details != nil && (details["selectedOptions"] != nil || details["customInput"] != nil) {
		fill(q.Questions[0].ID, details)
	}
	return out
}

// closeAsks answers every open ask of a turn with nothing: the turn ended
// (interrupted, failed) and the dialog is gone.
func (s *session) closeAsks(tid string) []harness.Item {
	var out []harness.Item
	for callID, it := range s.asks {
		if it.TurnID != tid {
			continue
		}
		body := *it.Question
		body.Answered = map[string][]string{}
		for _, qu := range body.Questions {
			body.Answered[qu.ID] = []string{}
		}
		it.Question = &body
		out = append(out, *it)
		delete(s.asks, callID)
	}
	return out
}
