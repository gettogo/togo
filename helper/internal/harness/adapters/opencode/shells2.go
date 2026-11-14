package opencode

import (
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode 2.x runs shell commands outside the turn: the shell tool with
// background set, a command sent to the background, POST /api/shell. The
// event stream tells of each (seen on 2.2.5): shell.created {info: {id,
// status, command, cwd, metadata, time.started}}, then shell.exited {id,
// status, exit} and, when it is removed, shell.deleted {id}. The tool tags
// a shell with the session that started it (metadata.sessionID).
//
// A running shell of this chat's sessions is a row of the background
// section, under OpenCode's own id. Like OpenCode's list (/api/shell, the
// terminal's shell tab) the section holds what runs: a shell that ended
// leaves it. The adapter's shells source lists the same ids, so a shell
// started while the stream was down, or before a helper restart, is still
// shown.

// shell2 is a shell as shell.created describes it.
type shell2 struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Command  string `json:"command"`
	Metadata struct {
		SessionID string `json:"sessionID"`
	} `json:"metadata"`
	Time struct {
		Started int64 `json:"started"`
	} `json:"time"`
}

// maxEndedShells bounds the ended ids remembered against a late start.
const maxEndedShells = 64

func (e *Events2) shellCreated(d ev2) ([]harness.Item, []harness.Op) {
	in := d.Info
	if in == nil || in.ID == "" || in.Status != "running" || !e.follows(in.Metadata.SessionID) {
		return nil, nil
	}
	if e.shells[in.ID] {
		return nil, nil // the same event again
	}
	for _, id := range e.shellsEnded {
		if id == in.ID {
			return nil, nil // its exit arrived first
		}
	}
	if e.shells == nil {
		e.shells = map[string]bool{}
	}
	e.shells[in.ID] = true
	title := truncate(strings.Join(strings.Fields(in.Command), " "), harness.MaxTitle)
	if title == "" {
		title = "Shell"
	}
	row := harness.Row{ID: in.ID, Title: title, State: harness.StateRunning}
	if at := ms(in.Time.Started); !at.IsZero() {
		row.Started = ptr(at)
	}
	return nil, []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &row}}
}

// shellEnded takes a shell's row away on its exit or its removal,
// whichever comes first.
func (e *Events2) shellEnded(id string) ([]harness.Item, []harness.Op) {
	if id == "" {
		return nil, nil
	}
	if !e.shells[id] {
		// Not shown: if its start is still on the way, it must not open
		// a row that nothing would close.
		e.shellsEnded = append(e.shellsEnded, id)
		if len(e.shellsEnded) > maxEndedShells {
			e.shellsEnded = e.shellsEnded[len(e.shellsEnded)-maxEndedShells:]
		}
		return nil, nil
	}
	delete(e.shells, id)
	return nil, []harness.Op{{Op: harness.OpRemove, Section: harness.SectionBackground, ID: id}}
}

// dropShells takes every shell row away after the stream reconnected: an
// exit may have been sent to nobody, and a row kept on would stay for
// good. The shells source lists the ones that still run.
func (e *Events2) dropShells() []harness.Op {
	ids := make([]string, 0, len(e.shells))
	for id := range e.shells {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ops := make([]harness.Op, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, harness.Op{Op: harness.OpRemove, Section: harness.SectionBackground, ID: id})
	}
	e.shells = nil
	return ops
}

// forgetShells drops the last conversation's shells: the engine starts
// the mission over with the chat, so their rows are gone already.
func (e *Events2) forgetShells() {
	e.shells, e.shellsEnded = nil, nil
}
