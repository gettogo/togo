package goose

import (
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// movedTo is the row of this folder written last, when it was written
// after the followed one and no other Goose runs in the folder to have
// written it (source.Solo, asked after the row was first seen): the agent
// moved to it. Otherwise none.
func (m *db) movedTo() sessionRow {
	cur := m.sessions[m.state["harness_session"]]
	var top sessionRow
	for _, c := range m.sessions {
		if c.ID == cur.ID || c.Updated <= cur.Updated {
			continue
		}
		if top.ID == "" || c.Updated > top.Updated || (c.Updated == top.Updated && c.ID > top.ID) {
			top = c
		}
	}
	if top.ID == "" {
		return sessionRow{}
	}
	since, ok := m.seen[top.ID]
	if !ok {
		since = time.Now()
		m.seen[top.ID] = since
	}
	// A lookup begun after the row was first seen; it runs away from the
	// session's lock and wakes the engine when done.
	if !m.solo.Alone(m.env, since, m.wake) {
		return sessionRow{}
	}
	return top
}

// pick chooses the session this Togo session's agent created. A row made
// before this session started (more than a minute, for clock skew) is
// another conversation that was only touched since; it is never taken. Of
// the rest, the one made after the session began and first is the row the
// TUI created at launch; otherwise the newest updated. Rows with no
// readable creation time are all candidates.
func (m *db) pick() sessionRow {
	started, _ := time.Parse(time.RFC3339, m.env.Vars["created"])
	var best sessionRow
	var bestAt time.Time
	bestFresh := false
	for _, c := range m.sessions {
		made, err := time.Parse(time.DateTime, c.Created)
		known := err == nil && !started.IsZero()
		if known && made.Before(started.Add(-time.Minute)) {
			continue
		}
		fresh := known && !made.Before(started)
		switch {
		case best.ID == "", fresh && !bestFresh:
		case fresh != bestFresh:
			continue
		case fresh && (made.Before(bestAt) || (made.Equal(bestAt) && c.ID < best.ID)):
		case !fresh && (c.Updated > best.Updated || (c.Updated == best.Updated && c.ID > best.ID)):
		default:
			continue
		}
		best, bestAt, bestFresh = c, made, fresh
	}
	return best
}

// Relaunched learns the session a relaunch resumes (`--session-id <id>`).
func (m *db) Relaunched(argv []string, _ time.Time) []harness.Op {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := sessionArg(argv)
	// A relaunch names the conversation it opens, or starts a new one:
	// what was followed before is not remembered past it.
	source.SaveFollowed(m.env, followName, id)
	if id != "" {
		m.pin = id
		if _, ok := m.sessions[id]; ok {
			m.session(m.sessions[id])
		} else if m.state["harness_session"] != id {
			// The feed lists only rows touched since this Togo session
			// began, which a resumed conversation may not be.
			m.b.Reset()
			m.asks, m.early = map[string]harness.Item{}, map[string]map[string][]string{}
			m.state["harness_session"] = id
			m.shown = false
		}
	}
	return nil
}

// sessionArg is the id a `--session-id <id>` (or `--session-id=<id>`) names.
func sessionArg(argv []string) string {
	for i, a := range argv {
		if v, ok := strings.CutPrefix(a, "--session-id="); ok {
			return v
		}
		if a == "--session-id" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}
