package engine

import (
	"encoding/json"
	"slices"

	"github.com/gettogo/togo/helper/internal/harness"
)

// missionState holds one session's mission control snapshot.
type missionState struct {
	rev      int64
	summary  harness.Summary
	sections []harness.Section // supported kinds in SectionOrder
	working  bool
}

// sectionCaps names the capability that turns each section on.
var sectionCaps = map[string][]string{
	harness.SectionNeeds:      {harness.CapNeedsApprovals, harness.CapNeedsQuestions},
	harness.SectionAgents:     {harness.CapAgents},
	harness.SectionBackground: {harness.CapBackground},
	harness.SectionPlan:       {harness.CapPlan},
	harness.SectionChanges:    {harness.CapChanges},
	harness.SectionPorts:      {harness.CapPorts},
	harness.SectionStatus:     {harness.CapStatus},
}

func newMission(caps []string) *missionState {
	m := &missionState{}
	for _, kind := range harness.SectionOrder {
		for _, c := range sectionCaps[kind] {
			if harness.Has(caps, c) {
				sec := harness.Section{Kind: kind, Rows: []harness.Row{}}
				m.sections = append(m.sections, sec)
				break
			}
		}
	}
	m.summary = m.computeSummary()
	return m
}

func (m *missionState) section(kind string) *harness.Section {
	for i := range m.sections {
		if m.sections[i].Kind == kind {
			return &m.sections[i]
		}
	}
	return nil
}

// apply applies ops and returns the delta, or nil when nothing changed.
// Ops for unsupported sections are dropped. A summary that changes because
// of the ops rides in the same delta.
func (m *missionState) apply(ops []harness.Op) *harness.Delta {
	var applied []harness.Op
	for _, op := range ops {
		if m.applyOne(op) {
			applied = append(applied, op)
		}
	}
	if s := m.computeSummary(); !sameSummary(s, m.summary) {
		m.summary = s
		sum := s
		applied = append(applied, harness.Op{Op: harness.OpSummary, Summary: &sum})
	}
	if len(applied) == 0 {
		return nil
	}
	m.rev++
	return &harness.Delta{Rev: m.rev, Ops: applied}
}

func (m *missionState) applyOne(op harness.Op) bool {
	switch op.Op {
	case harness.OpStatus:
		sec := m.section(harness.SectionStatus)
		if sec == nil || op.Status == nil {
			return false
		}
		if sameJSON(sec.Status, op.Status) {
			return false
		}
		st := *op.Status
		sec.Status = &st
		return true
	case harness.OpSummary:
		return false // computed here, never taken from a mapper
	}
	sec := m.section(op.Section)
	if sec == nil {
		return false
	}
	switch op.Op {
	case harness.OpUpsert:
		if op.Row == nil {
			return false
		}
		for i := range sec.Rows {
			if sec.Rows[i].ID == op.Row.ID {
				if sameJSON(sec.Rows[i], *op.Row) {
					return false
				}
				sec.Rows[i] = *op.Row
				return true
			}
		}
		sec.Rows = append(sec.Rows, *op.Row)
		return true
	case harness.OpRemove:
		n := len(sec.Rows)
		sec.Rows = slices.DeleteFunc(sec.Rows, func(r harness.Row) bool { return r.ID == op.ID })
		return len(sec.Rows) != n
	case harness.OpReplace:
		rows := op.Rows
		if rows == nil {
			rows = []harness.Row{}
		}
		if sameJSON(sec.Rows, rows) {
			return false
		}
		sec.Rows = slices.Clone(rows)
		return true
	}
	return false
}

// setWorking records whether a turn runs; the change, if any, is a delta.
func (m *missionState) setWorking(w bool) *harness.Delta {
	if m.working == w {
		return nil
	}
	m.working = w
	return m.apply(nil)
}

func (m *missionState) computeSummary() harness.Summary {
	s := harness.Summary{Working: m.working}
	if sec := m.section(harness.SectionNeeds); sec != nil {
		s.NeedsYou = len(sec.Rows)
	}
	if sec := m.section(harness.SectionAgents); sec != nil {
		s.Agents = countRunning(sec.Rows)
	}
	if sec := m.section(harness.SectionBackground); sec != nil {
		s.Background = countRunning(sec.Rows)
	}
	if sec := m.section(harness.SectionPlan); sec != nil && len(sec.Rows) > 0 {
		p := harness.Progress{Total: len(sec.Rows)}
		for _, r := range sec.Rows {
			if r.State == harness.RowDone || r.State == harness.RowSkipped {
				p.Done++
			}
		}
		s.Plan = &p
	}
	return s
}

func countRunning(rows []harness.Row) int {
	n := 0
	for _, r := range rows {
		if r.State == harness.StateRunning || r.State == harness.StateWaiting {
			n++
		}
	}
	return n
}

// snapshot returns a deep copy.
func (m *missionState) snapshot() harness.Mission {
	out := harness.Mission{Rev: m.rev, Summary: m.summary, Sections: []harness.Section{}}
	b, _ := json.Marshal(m.sections)
	_ = json.Unmarshal(b, &out.Sections)
	for i := range out.Sections {
		if out.Sections[i].Rows == nil {
			out.Sections[i].Rows = []harness.Row{}
		}
	}
	if m.summary.Plan != nil {
		p := *m.summary.Plan
		out.Summary.Plan = &p
	}
	return out
}

func sameSummary(a, b harness.Summary) bool { return sameJSON(a, b) }

func sameJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
