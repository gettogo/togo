package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Integration states, the one word setup shows per harness.
const (
	StateReady        = "ready"
	StateNeedsSetup   = "needsSetup"
	StateOutdated     = "outdated"
	StateNotNeeded    = "notNeeded"
	StateNotSupported = "notSupported"
	StateNotInstalled = "notInstalled"
)

// Status line states for a harness whose status line Togo chains.
const (
	LineChained = "chained" // the user's own, run after Togo's
	LineTogo    = "togo"    // Togo's own; the user had none
	LineOwn     = "own"     // the user's own, not chained
	LineAbsent  = "absent"
)

// Report is one harness's setup state, as `togo doctor` prints it and
// integrations.status returns it.
type Report struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Setup     Setup  `json:"setup"`
	State     string `json:"state"`
	// Hooks is Status's word for a harness that takes hooks.
	Hooks string `json:"hooks,omitempty"`
	// StatusLine is set for a harness whose status line Togo chains.
	StatusLine string `json:"statusLine,omitempty"`
	Path       string `json:"path,omitempty"`
	Backup     string `json:"backup,omitempty"`
	// LastHookAt is when the daemon last received one of the harness's
	// hooks; StatusLineAt, its status line.
	LastHookAt   string `json:"lastHookAt,omitempty"`
	StatusLineAt string `json:"statusLineAt,omitempty"`
	// Fix is the command that sets the harness up, when it needs one.
	Fix string `json:"fix,omitempty"`
}

// Reports describes every registry harness. seen holds the daemon's
// last-hook times (LoadSeen); nil leaves them out.
func Reports(o Options, seen Seen) []Report {
	out := make([]Report, 0, len(Registry))
	for _, h := range Registry {
		out = append(out, report(o, h, seen))
	}
	return out
}

func report(o Options, h Harness, seen Seen) Report {
	r := Report{ID: h.ID, Name: h.Name, Setup: h.Setup, Installed: h.Present(o)}
	if t, ok := seen[h.ID]; ok {
		r.LastHookAt = t.UTC().Format(time.RFC3339)
	}
	if t, ok := seen[h.ID+SeenStatusLine]; ok {
		r.StatusLineAt = t.UTC().Format(time.RFC3339)
	}
	switch {
	case !r.Installed:
		r.State = StateNotInstalled
		return r
	case h.Setup == SetupNone:
		r.State = StateNotNeeded
		return r
	case h.Setup == SetupPending:
		r.State = StateNotSupported
		return r
	}
	r.Path = h.Config(o)
	if fileExists(r.Path + backupSuffix) {
		r.Backup = r.Path + backupSuffix
	}
	r.Hooks = status(o, h.ID)
	lineOK := true
	if h.StatusLine {
		r.StatusLine = statusLineOf(r.Path)
		lineOK = r.StatusLine == LineChained || r.StatusLine == LineTogo
	}
	switch {
	case r.Hooks == StatusOutdated:
		r.State = StateOutdated
	case r.Hooks == StatusInstalled && lineOK:
		r.State = StateReady
	default:
		r.State = StateNeedsSetup
	}
	if r.State != StateReady {
		r.Fix = "togo hooks install --agent " + h.ID
	}
	return r
}

// statusLineOf reads Claude's statusLine command from settings.json.
func statusLineOf(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return LineAbsent
	}
	var doc struct {
		StatusLine *struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.StatusLine == nil || doc.StatusLine.Command == "" {
		return LineAbsent
	}
	switch c := doc.StatusLine.Command; {
	case chained(c):
		return LineChained
	case owned(c, Claude):
		return LineTogo
	}
	return LineOwn
}

// SeenStatusLine suffixes a harness id in Seen for its status line.
const SeenStatusLine = ".statusline"

// Seen maps a harness id (or id+SeenStatusLine) to the last time the
// daemon received its hook.
type Seen map[string]time.Time

// SeenPath is where the daemon keeps Seen under TOGO_HOME.
func SeenPath(togoHome string) string { return filepath.Join(togoHome, "hooks", "seen.json") }

// LoadSeen reads the daemon's last-hook times; a missing or broken file is
// an empty record.
func LoadSeen(togoHome string) Seen {
	s := Seen{}
	if togoHome == "" {
		return s
	}
	if b, err := os.ReadFile(SeenPath(togoHome)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Save writes the record atomically.
func (s Seen) Save(togoHome string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(SeenPath(togoHome), append(b, '\n'), 0o600)
}
