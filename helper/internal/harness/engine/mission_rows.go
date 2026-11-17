package engine

import (
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Mission control is for what is going on, not for what once happened: a
// conversation of two days showed twenty finished agents above the one that
// ran, and shells nobody could tell were alive. An agent in a terminal shows
// its helpers while they work and drops them when they are done; so does
// this list.
//
//   - A row that runs, waits or is ready stays for as long as it does.
//   - A row that ended well leaves at once. What it did is in the chat.
//   - A row that failed or was stopped stays, so that an empty list means
//     nothing runs and nothing went wrong, until the user's next prompt.
//   - A state this file does not know stays: a harness with words of its own
//     never loses a row that may still be alive.
//
// The rule sits here, on the sections every adapter writes to, so it holds
// for all of them and for a replay after a restart alike: replayed prompts
// clear the failures before them, and what is left is what runs and what
// failed since the last prompt.

// rowFate is what becomes of an agents or background row in a state.
type rowFate int

const (
	rowStays rowFate = iota // running, or not known: shown
	rowGone                 // ended well: removed
	rowHeld                 // failed or stopped: shown until the next prompt
)

// fateOf sorts a row state. Adapters use the harness package's states; a
// source-driven list (omp's jobs) passes the program's own words through.
func fateOf(state string) rowFate {
	switch strings.ToLower(state) {
	case harness.StateOK, "done", "completed", "complete", "finished", "succeeded", "success", "exited":
		return rowGone
	case harness.StateFailed, harness.StateInterrupted, harness.StateCancelled, harness.StateDenied,
		"canceled", "error", "errored", "killed", "timeout", "timed_out", "aborted", "crashed":
		return rowHeld
	}
	return rowStays
}

// livesByState reports whether rows of the section come and go by state.
func livesByState(section string) bool {
	return section == harness.SectionAgents || section == harness.SectionBackground
}

// settle turns an op for an agents or background row into what the list
// takes: an ended row's upsert becomes its removal, a failed row is noted
// for the next prompt, and a replaced list keeps only rows still shown.
func (m *missionState) settle(op harness.Op) harness.Op {
	if !livesByState(op.Section) {
		return op
	}
	switch op.Op {
	case harness.OpUpsert:
		if op.Row == nil {
			return op
		}
		switch fateOf(op.Row.State) {
		case rowGone:
			m.unhold(op.Section, op.Row.ID)
			return harness.Op{Op: harness.OpRemove, Section: op.Section, ID: op.Row.ID}
		case rowHeld:
			m.hold(op.Section, op.Row.ID)
		default:
			m.unhold(op.Section, op.Row.ID)
		}
	case harness.OpRemove:
		m.unhold(op.Section, op.ID)
	case harness.OpReplace:
		rows := make([]harness.Row, 0, len(op.Rows))
		delete(m.held, op.Section)
		for _, r := range op.Rows {
			switch fateOf(r.State) {
			case rowGone:
				continue
			case rowHeld:
				m.hold(op.Section, r.ID)
			}
			rows = append(rows, r)
		}
		op.Rows = rows
	}
	return op
}

func (m *missionState) hold(section, id string) {
	if m.held == nil {
		m.held = map[string]map[string]bool{}
	}
	if m.held[section] == nil {
		m.held[section] = map[string]bool{}
	}
	m.held[section][id] = true
}

func (m *missionState) unhold(section, id string) {
	delete(m.held[section], id)
}

// prompted removes the rows that failed or were stopped before the user's
// next prompt: they were there to be seen, and the new turn starts clean.
func (m *missionState) prompted() *harness.Delta {
	var ops []harness.Op
	for _, section := range []string{harness.SectionAgents, harness.SectionBackground} {
		sec := m.section(section)
		if sec == nil {
			continue
		}
		for _, r := range sec.Rows {
			if m.held[section][r.ID] {
				ops = append(ops, harness.Op{Op: harness.OpRemove, Section: section, ID: r.ID})
			}
		}
	}
	m.held = nil
	if len(ops) == 0 {
		return nil
	}
	return m.apply(ops)
}
