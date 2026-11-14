package omp

import (
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp can run a tool call in the background (bash with async: true). The
// call's result says so in details.async {state: "running", jobId, type},
// and when the job settles omp adds a custom_message of customType
// "async-result" whose details.jobs list {jobId, type, status, label,
// durationMs} (seen on 18.20). These jobs live in the session, not under
// `omp ps`, which lists supervised services only: they are mapped here so a
// running one shows in the background section and counts there.

// jobStarted opens a background job for a tool result that left one running.
func (s *session) jobStarted(tool *harness.Item, details map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	async := obj(details["async"])
	id := str(async, "jobId")
	if id == "" || str(async, "state") != "running" || s.jobs[id] != nil {
		return nil, nil
	}
	b := &harness.BackgroundBody{JobID: id, Kind: firstNonEmpty(str(async, "type"), "job"), Title: tool.Tool.Title,
		State: harness.StateRunning, Started: at}
	if sub := tool.Tool.Subject; sub != nil {
		b.Command = sub.Text
	}
	it := &harness.Item{ID: "job-" + id, TurnID: tool.TurnID, Kind: harness.KindBackground, At: at, Background: b}
	s.jobs[id] = it
	return []harness.Item{*it}, []harness.Op{jobRow(it)}
}

// jobsSettled closes the jobs an async-result message reports. A job the
// mapper never saw start (its call was read before this mapper existed)
// is shown finished, so the section never keeps a job omp says is over.
func (s *session) jobsSettled(e map[string]any, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	for _, j := range asList(obj(e["details"])["jobs"]) {
		jm := obj(j)
		id := str(jm, "jobId")
		if id == "" {
			continue
		}
		it := s.jobs[id]
		if it == nil {
			label := truncate(str(jm, "label"), harness.MaxTitle)
			it = &harness.Item{ID: "job-" + id, TurnID: s.turnID(), Kind: harness.KindBackground, At: at,
				Background: &harness.BackgroundBody{JobID: id, Kind: firstNonEmpty(str(jm, "type"), "job"), Title: firstNonEmpty(label, id), Command: label, Started: at}}
		}
		b := *it.Background
		b.Ended = ptr(at)
		switch str(jm, "status") {
		case "completed":
			b.State = harness.StateOK
		case "cancelled", "canceled", "killed", "aborted":
			b.State = harness.StateCancelled
		default:
			b.State = harness.StateFailed
		}
		it.Background = &b
		delete(s.jobs, id)
		items = append(items, *it)
		ops = append(ops, jobRow(it))
	}
	return items, ops
}

func jobRow(it *harness.Item) harness.Op {
	b := it.Background
	state := harness.RowSkipped
	switch b.State {
	case harness.StateRunning:
		state = harness.RowActive
	case harness.StateOK:
		state = harness.RowDone
	}
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &harness.Row{ID: it.ID, Title: b.Title,
		Subtitle: b.Command, State: state, Started: ptr(b.Started)}}
}
