// Package shell is the adapter for plain shell sessions: the tap's OSC 133
// command marks become one shell item per command.
package shell

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/tap"
)

//go:embed shell.yaml
var adapterFile []byte

// MapperMarks is the mapper shell.yaml names for its marks feed.
const MapperMarks = "shell.marks"

// Event is the hook event name the daemon gives forwarded shell marks.
const Event = tap.SignalShell

func init() {
	harness.RegisterAdapterFile("shell", adapterFile)
	harness.RegisterMapper(MapperMarks, func(env harness.Env) harness.Mapper { return newMarks(env) })
}

// marks turns start and end marks into shell items. A block keeps its id
// from start to end, so the app replaces the running block with the
// finished one in place.
type marks struct {
	env  harness.Env
	n    int
	open *harness.Item
	cwd  string
}

func newMarks(env harness.Env) *marks {
	return &marks{env: env, cwd: env.Cwd}
}

func (m *marks) Name() string { return MapperMarks }

func (m *marks) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var mk tap.Mark
	if err := json.Unmarshal(rec.Data, &mk); err != nil {
		return nil, nil
	}
	at := rec.At
	if at.IsZero() {
		at = time.Now()
	}
	if mk.Cwd != "" {
		m.cwd = mk.Cwd
	}
	switch mk.Mark {
	case tap.MarkStart:
		var out []harness.Item
		if m.open != nil {
			// A start without the previous end: that command is over.
			out = append(out, m.finish(nil, "", at))
		}
		m.n++
		item := harness.Item{
			ID:   fmt.Sprintf("cmd%d", m.n),
			Kind: harness.KindShell,
			At:   at,
			Shell: &harness.ShellBody{
				Command: limit(mk.Command, harness.MaxItemText),
				Cwd:     m.cwd,
				State:   harness.StateRunning,
				Started: at,
			},
		}
		m.open = &item
		return append(out, item), nil
	case tap.MarkEnd:
		if m.open != nil {
			if mk.Command != "" && m.open.Shell.Command == "" {
				m.open.Shell.Command = limit(mk.Command, harness.MaxItemText)
			}
			return []harness.Item{m.finish(mk.Exit, mk.Output, at)}, nil
		}
		if strings.TrimSpace(mk.Command) == "" {
			return nil, nil
		}
		m.n++
		item := harness.Item{
			ID:   fmt.Sprintf("cmd%d", m.n),
			Kind: harness.KindShell,
			At:   at,
			Shell: &harness.ShellBody{
				Command: limit(mk.Command, harness.MaxItemText),
				Cwd:     m.cwd,
				Started: at,
			},
		}
		m.open = &item
		return []harness.Item{m.finish(mk.Exit, mk.Output, at)}, nil
	}
	return nil, nil
}

// finish closes the open block. A block whose shell reported no status
// counts as done.
func (m *marks) finish(exit *int, output string, at time.Time) harness.Item {
	item := *m.open
	body := *item.Shell
	m.open = nil
	body.State = harness.StateOK
	if exit != nil {
		code := *exit
		body.ExitCode = &code
		if code != 0 {
			body.State = harness.StateFailed
		}
	}
	ended := at
	body.Ended = &ended
	body.Output = tail(output, harness.MaxToolOutput)
	item.Shell = &body
	return item
}

// State reports whether a command runs and the shell's directory.
func (m *marks) State() map[string]string {
	working := "false"
	if m.open != nil {
		working = "true"
	}
	return map[string]string{"working": working, "cwd": m.cwd}
}

// limit keeps the first n bytes of s at a character boundary.
func limit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// tail keeps the last n bytes of s at a character boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}
