package omp

import (
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A subagent file read before the call that starts its task (the feeds run
// apart; after a restart either can come first) gets its row at once,
// under the id the call gives its task, agent-<task name>. The call adopts
// that row when it is read: it names it and places it in its turn, and the
// file's steps are already under it. So the result does not depend on
// which feed is read first, nothing is held back waiting, and nothing
// depends on another record arriving.
//
// A file written long before it is read (after a restart, most are an
// earlier turn's, whose call is outside what was read of the conversation)
// is not shown until its call adopts it: its row and steps are kept, as
// items, and given out on adoption. When the conversation is read past the
// file's start without its call, the call is not in what was read: the
// kept items are let go and the file stays out of view.

type earlyFile struct {
	at     time.Time // the file's header time
	hidden bool      // an old file: not shown until its call adopts it
	gone   bool      // hidden, and the conversation was read past it
	items  map[string]harness.Item
	order  []string
}

// earlyRow gives a file no call names yet its own row, agent-<task name>.
func (s *session) earlyRow(id string, e map[string]any, at time.Time, file string, old bool) ([]harness.Item, []harness.Op) {
	name := taskName(file)
	aid := "agent-" + name
	if ef := s.byFile.early[aid]; ef != nil {
		// The file written again from its start (a new run of the task).
		ef.at = at
		s.byFile.headerAt[aid] = at
		return s.bound(id, aid, file)
	}
	if s.agents[aid] != nil {
		aid = "agent-" + id // the name is another row's id: a row of its own
	}
	it := harness.Item{ID: aid, Kind: harness.KindAgent, At: at, TurnID: s.turnID(),
		Agent: &harness.AgentBody{AgentID: strings.TrimPrefix(aid, "agent-"), Name: firstNonEmpty(str(e, "title"), name), State: harness.StateRunning, Started: at}}
	s.agents[aid] = &it
	s.byFile.early[aid] = &earlyFile{at: at, hidden: old, items: map[string]harness.Item{}}
	s.byFile.headerAt[aid] = at
	return s.bound(id, aid, file)
}

// screen keeps back what belongs to a hidden early row: its row and steps
// are kept (until the conversation is read past the file) instead of shown.
func (f *agentFiles) screen(items []harness.Item, ops []harness.Op) ([]harness.Item, []harness.Op) {
	hiddenAt := func(id string) *earlyFile {
		if ef := f.early[id]; ef != nil && ef.hidden {
			return ef
		}
		return nil
	}
	keep := items[:0:0]
	for _, it := range items {
		ef := hiddenAt(it.ID)
		if ef == nil {
			ef = hiddenAt(it.Parent)
		}
		if ef == nil {
			keep = append(keep, it)
			continue
		}
		if !ef.gone {
			if _, seen := ef.items[it.ID]; !seen {
				ef.order = append(ef.order, it.ID)
			}
			ef.items[it.ID] = it
		}
	}
	keepOps := ops[:0:0]
	for _, op := range ops {
		if op.Section == harness.SectionAgents && op.Row != nil && hiddenAt(op.Row.ID) != nil {
			continue
		}
		keepOps = append(keepOps, op)
	}
	return keep, keepOps
}

// passed lets go of the kept items of hidden files the conversation was
// read past: their call is not in what was read.
func (f *agentFiles) passed() {
	for _, ef := range f.early {
		if ef.hidden && !ef.gone && f.mainAt.After(ef.at) {
			ef.gone, ef.items, ef.order = true, nil, nil
		}
	}
}

// adopt gives a task call the row made early from its file, named and
// placed as the call's own, with the steps kept for it. ok is false when
// there is no such row.
func (s *session) adopt(aid, callID string, t task, tid string, at time.Time) ([]harness.Item, []harness.Op, bool) {
	ef := s.byFile.early[aid]
	if ef == nil {
		return nil, nil, false
	}
	delete(s.byFile.early, aid)
	a := s.agents[aid]
	ab := *a.Agent
	ab.Name, ab.Role, ab.Started = truncate(t.title, harness.MaxTitle), t.role, at
	a.Agent, a.TurnID, a.At = &ab, tid, at
	s.taskCalls[callID] = append(s.taskCalls[callID], aid)
	s.byFile.named(t.id, aid)
	items := []harness.Item{*a}
	for _, id := range ef.order {
		if id != aid {
			items = append(items, ef.items[id])
		}
	}
	return items, []harness.Op{s.agentRow(a)}, true
}

// takeOver is for a call that uses a task name an earlier call used, whose
// row holds the name's file: omp keeps one file per name, so a file dated
// after this call is this call's run. This call takes the row with its
// steps; the earlier call's row moves to an id of its own. ok is false when
// the file is the earlier call's.
func (s *session) takeOver(aid, callID string, t task, tid string, at time.Time) ([]harness.Item, []harness.Op, bool) {
	hdr, bound := s.byFile.headerAt[aid]
	a := s.agents[aid]
	if !bound || a == nil || hdr.Before(at) || !a.Agent.Started.Before(at) {
		return nil, nil, false
	}
	prev := ""
	for c, ids := range s.taskCalls {
		if slices.Contains(ids, aid) {
			prev = c
		}
	}
	if prev == "" {
		return nil, nil, false
	}
	moved := *a
	moved.ID = "agent-" + prev + "-" + strings.TrimPrefix(aid, "agent-")
	mb := *a.Agent
	mb.AgentID = strings.TrimPrefix(moved.ID, "agent-")
	moved.Agent = &mb
	s.agents[moved.ID] = &moved
	s.taskCalls[prev] = replace(s.taskCalls[prev], aid, moved.ID)
	s.byFile.names[t.id] = replace(s.byFile.names[t.id], aid, moved.ID)
	if s.detached[aid] {
		s.detached[moved.ID] = true
	}
	ab := *a.Agent
	ab.Name, ab.Role, ab.Started = truncate(t.title, harness.MaxTitle), t.role, at
	a.Agent, a.TurnID, a.At = &ab, tid, at
	delete(s.detached, aid)
	s.taskCalls[callID] = append(s.taskCalls[callID], aid)
	s.byFile.named(t.id, aid)
	return []harness.Item{moved, *a}, []harness.Op{s.agentRow(&moved), s.agentRow(a)}, true
}

func replace(ids []string, from, to string) []string {
	out := slices.Clone(ids)
	for i, id := range out {
		if id == from {
			out[i] = to
		}
	}
	return out
}
