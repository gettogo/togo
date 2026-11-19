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
// task by its name, never by the order files arrive in. A file read before
// the call that starts its task gets its row at once, under the id the
// call gives it, and the call adopts that row (early.go): nothing waits.

// agentFiles is what the mapper knows of the subagent files: which task
// each one is, and the rows made from files no call named yet. Its life is
// the mapper's: the engine builds a new mapper when the conversation
// changes (/new, /resume), the session is removed or the helper restarts,
// and every file is then read again.
type agentFiles struct {
	// names lists the agent item ids of each task name, in call order (a
	// later call can use a name again). Set by a task call; kept as long as
	// the agent rows it names, which are kept across a re-read.
	names map[string][]string
	// agent is the agent item id (or dropped) a file's header was given.
	// Set by the header; kept like headers.
	agent map[string]string
	// headerAt is the header time of the file an agent row was given. Set
	// with agent; a later header of the same file replaces it.
	headerAt map[string]time.Time
	// early holds the rows made from a file before any call named its
	// task (early.go). Cleared when the call adopts the row; an old file's
	// kept items are let go when the conversation is read past it.
	early map[string]*earlyFile
	// mainAt is the newest time of a main conversation entry read. Reset
	// when the main conversation is read again from its start.
	mainAt time.Time
}

func newAgentFiles() agentFiles {
	return agentFiles{names: map[string][]string{}, agent: map[string]string{},
		headerAt: map[string]time.Time{}, early: map[string]*earlyFile{}}
}

// named records a task call's agent for a task name.
func (f *agentFiles) named(name, aid string) {
	if name != "" && !slices.Contains(f.names[name], aid) {
		f.names[name] = append(f.names[name], aid)
	}
}

// mainRead notes the time of a main conversation entry that has its own.
func (f *agentFiles) mainRead(e map[string]any, at time.Time) {
	if _, ok := e["timestamp"]; ok && at.After(f.mainAt) {
		f.mainAt = at
	}
}

// taskName is the task a subagent file is named after.
func taskName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// taskFor is the agent row a file of task name, whose header is dated at,
// belongs to: the row of the latest call of that name started at or
// before the file (a name used again has one file, holding the latest
// run); with none so dated, the earliest still waiting for a file, else the
// latest call's.
func (s *session) taskFor(name string, at time.Time) string {
	ids := s.byFile.names[name]
	best := ""
	for _, id := range ids {
		if a := s.agents[id]; a != nil && !a.Agent.Started.After(at) && (best == "" || !a.Agent.Started.Before(s.agents[best].Agent.Started)) {
			best = id
		}
	}
	if best != "" {
		return best
	}
	for _, p := range s.pending {
		if slices.Contains(ids, p) {
			return p
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

// subagentHeader starts a subagent's file: the row of the task it is named
// after; else, for a file no task call names, the first task still waiting
// for a file (omp versions that named files otherwise), or a row of its own
// that the call adopts when it is read (early.go).
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
		if aid := s.taskFor(taskName(file), at); aid != "" {
			// Earlier runs of the name wrote the same file: theirs is gone.
			var done []string
			for _, other := range s.byFile.names[taskName(file)] {
				if !s.agents[other].Agent.Started.After(s.agents[aid].Agent.Started) {
					done = append(done, other)
				}
			}
			s.dropPending(done)
			s.byFile.headerAt[aid] = at
			return s.bound(id, aid, file)
		}
	}
	if len(s.pending) > 0 {
		var agentID string
		agentID, s.pending = s.pending[0], s.pending[1:]
		return s.bound(id, agentID, file)
	}
	created, err := time.Parse(time.RFC3339, s.env.Vars["created"])
	// A file of an earlier session (read because its file was the newest),
	// or one written long before it is read (after a restart, an earlier
	// turn's): not a step of the turn that runs now.
	old := err == nil && at.Before(created.Add(-time.Minute)) || s.readAt.Sub(at) > agentFresh
	if file != "" {
		return s.earlyRow(id, e, at, file, old)
	}
	if old {
		return s.bound(id, dropped, file)
	}
	agentID := "agent-" + id
	it := harness.Item{ID: agentID, Kind: harness.KindAgent, At: at, TurnID: s.turnID(),
		Agent: &harness.AgentBody{AgentID: id, Name: firstNonEmpty(str(e, "title"), "Subagent"), State: harness.StateRunning, Started: at}}
	s.agents[agentID] = &it
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
