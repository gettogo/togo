package omp

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp writes each subagent's conversation to a file named after its task,
// <session>/<task name>.jsonl (seen on 18.19 to 18.21; earlier versions
// named a task by "id" and the file after it). The engine reads the main
// conversation and that folder as two feeds, in no fixed order, and the
// files of one look in no fixed order either. So a file is matched to its
// task by its name, never by the order files arrive in.

// agentFiles is what the mapper knows of the subagent files: which task
// each one is. Its life is the mapper's: the engine builds a new mapper
// when the conversation changes (/new, /resume), the session is removed or
// the helper restarts, and every file is then read again.
type agentFiles struct {
	// names lists the agent item ids of each task name, in call order (a
	// later call can use a name again). Set by a task call; kept as long as
	// the agent rows it names, which are kept across a re-read.
	names map[string][]string
	// agent is the agent item id (or dropped) a file's header was given.
	// Set by the header; kept like headers.
	agent map[string]string
}

func newAgentFiles() agentFiles {
	return agentFiles{names: map[string][]string{}, agent: map[string]string{}}
}

// named records a task call's agent for a task name.
func (f *agentFiles) named(name, aid string) {
	if name != "" && !slices.Contains(f.names[name], aid) {
		f.names[name] = append(f.names[name], aid)
	}
}

// taskName is the task a subagent file is named after.
func taskName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// taskFor is the agent row a file of task name belongs to: the earliest
// call's row still waiting for its file, else the latest call's row (a
// file read again, or read after its task ended).
func (f *agentFiles) taskFor(name string, pending []string) string {
	ids := f.names[name]
	if len(ids) == 0 {
		return ""
	}
	for _, p := range pending {
		if slices.Contains(ids, p) {
			return p
		}
	}
	return ids[len(ids)-1]
}

// subagentHeader starts a subagent's file: the row of the task it is named
// after; else, for a file no task call names, the first task still waiting
// for a file (omp versions that named files otherwise) or a row of its own.
func (s *session) subagentHeader(e map[string]any, at time.Time, file string) ([]harness.Item, []harness.Op) {
	id := str(e, "id")
	if a, ok := s.headers[id]; ok {
		s.bound(id, a, file) // read again: the row is as it was
		return nil, nil
	}
	if hs := s.state["harness_session"]; hs != "" && !strings.HasSuffix(str(e, "parentSession"), "_"+hs+".jsonl") {
		// A subagent of another omp's session in the same folder (one the
		// user ran outside Togo, or an earlier session of this pane).
		return s.bound(id, dropped, file)
	}
	if file != "" {
		if aid := s.byFile.taskFor(taskName(file), s.pending); aid != "" {
			s.dropPending([]string{aid})
			return s.bound(id, aid, file)
		}
	}
	if created, err := time.Parse(time.RFC3339, s.env.Vars["created"]); err == nil && len(s.pending) == 0 && at.Before(created.Add(-time.Minute)) {
		// A subagent of an earlier session, read because its file was
		// the newest: not part of this conversation.
		return s.bound(id, dropped, file)
	}
	if len(s.pending) == 0 && s.readAt.Sub(at) > agentFresh {
		// No task call is waiting for this file and it was written long
		// before it is read: after a restart the conversation is read from
		// its recent part and the subagent files whole, so this is one of
		// an earlier turn. It is not a step of the turn that runs now.
		return s.bound(id, dropped, file)
	}
	var agentID string
	if len(s.pending) > 0 {
		agentID, s.pending = s.pending[0], s.pending[1:]
	} else {
		agentID = "agent-" + id
		it := harness.Item{ID: agentID, Kind: harness.KindAgent, At: at, TurnID: s.turnID(),
			Agent: &harness.AgentBody{AgentID: id, Name: firstNonEmpty(str(e, "title"), "Subagent"), State: harness.StateRunning, Started: at}}
		s.agents[agentID] = &it
	}
	return s.bound(id, agentID, file)
}

// bound gives a header and its file to an agent row, and shows the row.
func (s *session) bound(id, aid, file string) ([]harness.Item, []harness.Op) {
	s.headers[id], s.cur = aid, aid
	if file != "" {
		s.byFile.agent[file] = aid
	}
	a := s.agents[aid]
	if aid == dropped || a == nil {
		return nil, nil
	}
	it := *a
	return []harness.Item{it}, []harness.Op{s.agentRow(&it)}
}
