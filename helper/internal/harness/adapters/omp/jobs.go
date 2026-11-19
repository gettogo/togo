package omp

import (
	"sort"
	"strings"
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
		if str(jm, "type") == "task" {
			if ai, ao := s.agentsSettled([]any{j}, at); len(ai) > 0 {
				items, ops = append(items, ai...), append(ops, ao...)
				continue
			}
			if s.taskAgent(firstNonEmpty(str(jm, "agentId"), id)) {
				// The agent's row has ended already (its own file recorded
				// its end, read first): it keeps how it ended, and is not
				// a job.
				continue
			}
		}
		it := s.jobs[id]
		if it == nil {
			label := truncate(str(jm, "label"), harness.MaxTitle)
			it = &harness.Item{ID: "job-" + id, TurnID: s.turnID(), Kind: harness.KindBackground, At: at,
				Background: &harness.BackgroundBody{JobID: id, Kind: firstNonEmpty(str(jm, "type"), "job"), Title: firstNonEmpty(label, id), Command: label, Started: at}}
		}
		b := *it.Background
		b.Ended = ptr(at)
		b.State = jobState(str(jm, "status"))
		it.Background = &b
		delete(s.jobs, id)
		items = append(items, *it)
		ops = append(ops, jobRow(it))
	}
	return items, ops
}

func jobRow(it *harness.Item) harness.Op {
	b := it.Background
	return harness.Op{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &harness.Row{ID: it.ID, Title: b.Title,
		Subtitle: b.Command, State: b.State, Started: ptr(b.Started)}}
}

// exited ends the background agents and jobs omp left running when it
// recorded its own exit (custom "session_exit", seen on 18.16 to 18.20).
// They ran inside omp, so nothing reports them again, and omp numbers its
// jobs from one after a resume.
func (s *session) exited(at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	for _, a := range s.agents {
		if a.Agent.Ended != nil {
			continue
		}
		ab := *a.Agent
		ab.State, ab.Ended = harness.StateInterrupted, ptr(at)
		a.Agent = &ab
		items = append(items, *a)
		ops = append(ops, s.agentRow(a))
	}
	for id, it := range s.jobs {
		b := *it.Background
		b.State, b.Ended = harness.StateInterrupted, ptr(at)
		it.Background = &b
		delete(s.jobs, id)
		items = append(items, *it)
		ops = append(ops, jobRow(it))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	sort.Slice(ops, func(i, j int) bool { return ops[i].Row.ID < ops[j].Row.ID })
	return items, ops
}

// agentExited ends a subagent whose own file records its exit (seen on
// 18.21: every subagent's file ends with session_exit). It is the one end
// that needs nothing from the main conversation, so it also closes a row
// made from a file read with no task call waiting for it, which no report
// of omp's names. An agent already ended keeps how it ended.
func (s *session) agentExited(aid, kind string, at time.Time) ([]harness.Item, []harness.Op) {
	a := s.agents[aid]
	if a == nil || a.Agent.Ended != nil {
		return nil, nil
	}
	ab := *a.Agent
	ab.State, ab.Ended = harness.StateOK, ptr(at)
	if kind != "" && kind != "normal" {
		ab.State = harness.StateInterrupted
	}
	a.Agent = &ab
	return []harness.Item{*a}, []harness.Op{s.agentRow(a)}
}

// jobState maps the status omp gives a settled job.
func jobState(status string) string {
	switch status {
	case "completed":
		return harness.StateOK
	case "cancelled", "canceled", "killed", "aborted":
		return harness.StateCancelled
	}
	return harness.StateFailed
}

// agentsSettled ends the background agents a list of omp jobs reports as
// over: an async-result's jobs {jobId, agentId, type: task, status}, or the
// job list a read of omp's jobs returns {id, type, status}. A job still
// running, one that is not a task, and an agent already ended are left.
func (s *session) agentsSettled(jobs []any, at time.Time) ([]harness.Item, []harness.Op) {
	var items []harness.Item
	var ops []harness.Op
	for _, j := range jobs {
		jm := obj(j)
		status := str(jm, "status")
		if str(jm, "type") != "task" || status == "" || status == "running" || status == "pending" {
			continue
		}
		a := s.runningAgent(firstNonEmpty(str(jm, "agentId"), str(jm, "jobId"), str(jm, "id")))
		if a == nil {
			continue
		}
		ab := *a.Agent
		ab.Ended = ptr(at)
		ab.State = jobState(status)
		a.Agent = &ab
		items = append(items, *a)
		ops = append(ops, s.agentRow(a))
	}
	return items, ops
}

// runningAgent finds the running agent row for a task id: the plain row, or
// the one a later call that reused the id was given.
func (s *session) runningAgent(id string) *harness.Item {
	if id == "" {
		return nil
	}
	if a := s.agents["agent-"+id]; a != nil && a.Agent.Ended == nil {
		return a
	}
	for aid, a := range s.agents {
		if a.Agent.Ended == nil && strings.HasPrefix(aid, "agent-") && strings.HasSuffix(aid, "-"+id) {
			return a
		}
	}
	return nil
}

// taskAgent reports whether a task call made an agent row for this task
// name, running or ended.
func (s *session) taskAgent(name string) bool {
	return name != "" && (len(s.byFile.names[name]) > 0 || s.agents["agent-"+name] != nil)
}

// agentFresh is how recent an agent's record must be, when it is read, to
// count as something the agent does now. A file replayed long after it was
// written tells what happened, not what is happening.
const agentFresh = 5 * time.Minute

// agentStopped follows a background agent by its own conversation. omp
// reports most ends itself, but not one whose session was interrupted
// first (seen on 18.19: the file ends "aborted" and no async-result
// follows), and that row would stay running.
//
// The agent's own word outranks omp's exit: read after a restart, the main
// file's recorded exit marks the agents of that time interrupted before
// their files, read next, say they had finished.
//
// A stop is not always the end (seen on 18.20): omp adds a note and the
// agent goes back to its tools. Its next tool call makes it running again,
// when that call was just made; an old one, in a file replayed later,
// leaves it as it ended.
func (s *session) agentStopped(aid, stopReason string, at time.Time) ([]harness.Item, []harness.Op) {
	a := s.agents[aid]
	if a == nil || !s.detached[aid] {
		return nil, nil
	}
	ab := *a.Agent
	ended := ab.Ended != nil
	switch stopReason {
	case "stop":
		if ended && ab.State != harness.StateInterrupted {
			return nil, nil
		}
		ab.State, ab.Ended = harness.StateOK, ptr(at)
	case "aborted":
		if ended {
			return nil, nil
		}
		ab.State, ab.Ended = harness.StateCancelled, ptr(at)
	case "toolUse":
		if !ended || ab.State != harness.StateOK || s.readAt.Sub(at) > agentFresh {
			return nil, nil
		}
		ab.State, ab.Ended = harness.StateRunning, nil
	default:
		return nil, nil
	}
	a.Agent = &ab
	return []harness.Item{*a}, []harness.Op{s.agentRow(a)}
}
