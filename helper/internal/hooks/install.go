// Package hooks adds and removes the entries that make each harness in the
// Registry that takes hooks (Claude Code, Codex, Auggie, Droid, Copilot
// CLI, Cursor CLI, and Amp through Togo's own plugin) report events to
// "togo hook <agent> <event>",
// and chains Claude Code's status line. It only ever touches entries whose
// command ends in "hook <agent> <event>", keeps every other key and hook in
// the harness's config, backs a file up once before its first change, and
// refuses to write a file it cannot parse or a Togo entry the user edited.
package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Agent names accepted by Install, Uninstall and Status.
const (
	Claude = "claude"
	Codex  = "codex"
)

// Status values reported per agent.
const (
	StatusInstalled = "installed"
	StatusAbsent    = "absent"
	// StatusOutdated: some of togo's entries are there, not all the
	// current version installs. Running install again adds the rest.
	StatusOutdated = "outdated"
	StatusNoAgent  = "no-agent"
)

const backupSuffix = ".togo-backup"

// Options locates the files to edit. Callers resolve the agent config roots
// from the environment, so tests point them at temporary directories.
type Options struct {
	TogoHome  string // holds hooks.json, the install record
	Binary    string // absolute path of the togo executable
	ClaudeDir string // $CLAUDE_CONFIG_DIR or ~/.claude
	CodexDir  string // $CODEX_HOME or ~/.codex
	// The other harnesses' config roots: ~/.augment, ~/.factory, ~/.copilot
	// and ~/.vibe, and ~/.gemini for removing Gemini CLI's retired hooks.
	GeminiDir  string
	AugmentDir string
	FactoryDir string
	CopilotDir string
	VibeDir    string
	CursorDir  string // ~/.cursor
	AmpDir     string // ~/.config/amp
	Cwd        string // project directory for Local
	Local      bool   // Claude: edit ./.claude/settings.local.json instead
	Block      bool   // also hook the approval event, so the phone can answer
	// NoStatusLine leaves Claude's status line as it is on install.
	NoStatusLine bool
	// HasBinary reports whether a harness's program is installed; nil
	// counts every program as installed, so only the config root decides.
	HasBinary func(command string) bool
}

// Result is the outcome for one agent. State is "installed", "unchanged",
// "removed", "skipped" (not installed here) or "refused" with Err. Backup
// is the copy of Path taken before Togo's first change, when there is one.
type Result struct {
	Agent  string
	Path   string
	State  string
	Backup string
	Err    error
}

// Install adds togo's hook entries for each named agent. Running it again
// with the same options changes nothing.
func Install(o Options, agents []string) []Result {
	return each(agents, func(agent string) Result { return install(o, agent) })
}

// Uninstall removes togo's hook entries for each named agent.
func Uninstall(o Options, agents []string) []Result {
	return each(agents, func(agent string) Result { return uninstall(o, agent) })
}

// Status reports "installed", "outdated", "absent" or "no-agent" per agent. Claude counts
// as installed when every base event has a togo entry.
func Status(o Options) map[string]string {
	out := map[string]string{}
	for _, agent := range Agents {
		out[agent] = status(o, agent)
	}
	return out
}

func each(agents []string, fn func(string) Result) []Result {
	var out []Result
	for _, a := range agents {
		out = append(out, fn(a))
	}
	return out
}

// edit is one file's change: transform maps current bytes (nil when the file
// is missing) to the new bytes.
type edit struct {
	path      string
	transform func(cur []byte) ([]byte, error)
}

func install(o Options, agent string) Result {
	edits, root, err := editsFor(o, agent, true)
	res := Result{Agent: agent}
	if len(edits) > 0 {
		res.Path = edits[0].path
	}
	h, _ := Lookup(agent)
	switch {
	case err != nil:
		res.State, res.Err = "refused", err
	case root == "" || !(o.Local && agent == Claude) && !h.Present(o):
		res.State = "skipped"
	default:
		res.State, res.Err = applyAll(o, edits, false)
	}
	if res.Path != "" {
		if _, err := os.Stat(res.Path + backupSuffix); err == nil {
			res.Backup = res.Path + backupSuffix
		}
	}
	return res
}

func uninstall(o Options, agent string) Result {
	edits, root, err := editsFor(o, agent, false)
	res := Result{Agent: agent}
	if len(edits) > 0 {
		res.Path = edits[0].path
	}
	switch {
	case err != nil:
		res.State, res.Err = "refused", err
	case root == "":
		res.State = "skipped"
	default:
		res.State, res.Err = applyAll(o, edits, true)
	}
	return res
}

// editsFor returns the edits for an agent and its config root, empty when the
// agent is not installed on this host.
func editsFor(o Options, agent string, add bool) ([]edit, string, error) {
	if o.Binary == "" && add {
		return nil, "", errors.New("togo binary path is empty")
	}
	switch agent {
	case Claude:
		return claudeEdits(o, add)
	case Codex:
		return codexEdits(o, add)
	case Droid:
		return droidEdits(o, add)
	case Copilot:
		return copilotEdits(o, add)
	case Gemini:
		return geminiEdits(o, add)
	case Auggie:
		return auggieEdits(o, add)
	case Cursor:
		return cursorEdits(o, add)
	case Amp:
		return ampEdits(o, add)
	}
	return nil, "", fmt.Errorf("unknown agent %q", agent)
}

// applyAll computes every file's new bytes before writing any, so a parse
// error in the second file leaves the first untouched.
func applyAll(o Options, edits []edit, undo bool) (string, error) {
	type pending struct {
		e        edit
		cur, out []byte
		existed  bool
	}
	var work []pending
	for _, e := range edits {
		cur, err := os.ReadFile(e.path)
		existed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "refused", fmt.Errorf("read %s: %w", e.path, err)
		}
		if undo && !existed {
			rec, err := loadRecords(o.TogoHome)
			if err != nil {
				return "refused", err
			}
			rec.drop(e.path)
			if err := rec.save(); err != nil {
				return "refused", err
			}
			continue
		}
		if undo {
			if restored, ok, err := tryRestore(o, e.path, cur); err != nil {
				return "refused", err
			} else if ok {
				work = append(work, pending{e: e, cur: cur, out: restored, existed: true})
				continue
			}
		}
		out, err := e.transform(cur)
		if err != nil {
			return "refused", fmt.Errorf("%s: %w", e.path, err)
		}
		work = append(work, pending{e: e, cur: cur, out: out, existed: existed})
	}
	state := "unchanged"
	for _, w := range work {
		changed, err := commit(o, w.e.path, w.cur, w.out, w.existed, undo)
		if err != nil {
			return "refused", err
		}
		if changed {
			state = "installed"
			if undo {
				state = "removed"
			}
		}
	}
	return state, nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// commit writes out in place of cur and keeps the install record in step. A nil out
// on uninstall means the file did not exist before togo and is deleted.
func commit(o Options, path string, cur, out []byte, existed, undo bool) (bool, error) {
	led, err := loadRecords(o.TogoHome)
	if err != nil {
		return false, err
	}
	if undo {
		led.drop(path)
		if out == nil {
			if err := os.Remove(path); err != nil {
				return false, fmt.Errorf("remove %s: %w", path, err)
			}
			_ = os.Remove(path + backupSuffix)
			return true, led.save()
		}
		if bytes.Equal(cur, out) {
			return false, led.save()
		}
		if err := writeAtomic(path, out, true); err != nil {
			return false, err
		}
		_ = os.Remove(path + backupSuffix)
		return true, led.save()
	}
	if bytes.Equal(cur, out) && existed {
		return false, nil
	}
	prev, tracked := led.find(path)
	ours := tracked && existed && prev.After == sum(cur)
	// The backup is taken once, before Togo's first change, and a later
	// install never overwrites it. A file the user edited since keeps that
	// first backup; uninstall then removes Togo's entries instead of
	// restoring it.
	if existed && !ours && !(tracked && fileExists(path+backupSuffix)) {
		if err := writeBackup(path, cur); err != nil {
			return false, err
		}
	}
	if err := writeAtomic(path, out, existed); err != nil {
		return false, err
	}
	before := ""
	if existed {
		before = sum(cur)
	}
	if ours {
		before = prev.Before
	}
	led.put(entry{Path: path, Before: before, After: sum(out)})
	return true, led.save()
}

// tryRestore returns the pre-install bytes when the file is exactly what
// togo wrote and the backup still matches the recorded original. A nil
// result with ok means the file did not exist before and should be deleted.
func tryRestore(o Options, path string, cur []byte) ([]byte, bool, error) {
	led, err := loadRecords(o.TogoHome)
	if err != nil {
		return nil, false, err
	}
	e, ok := led.find(path)
	if !ok || e.After != sum(cur) {
		return nil, false, nil
	}
	if e.Before == "" {
		return nil, true, nil
	}
	orig, err := os.ReadFile(path + backupSuffix)
	if err != nil || sum(orig) != e.Before {
		return nil, false, nil
	}
	return orig, true, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeBackup(path string, data []byte) error {
	return writeFileAtomic(path+backupSuffix, data, 0o600)
}

// writeAtomic replaces path through a temp file and rename, keeping the
// existing mode and following a symlink so dotfile managers keep working.
func writeAtomic(path string, data []byte, keepMode bool) error {
	mode := os.FileMode(0o600)
	target := path
	if keepMode {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			target = resolved
		}
		if st, err := os.Stat(target); err == nil {
			mode = st.Mode().Perm()
		}
	}
	return writeFileAtomic(target, data, mode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// recordFile lists, per edited file, the hash before and after togo's write so
// uninstall can restore the original bytes when nothing changed since.
type recordFile struct {
	path    string
	Entries []entry `json:"files"`
}

type entry struct {
	Path   string `json:"path"`
	Before string `json:"sha256Before"`
	After  string `json:"sha256After"`
}

func loadRecords(home string) (*recordFile, error) {
	l := &recordFile{path: filepath.Join(home, "hooks.json")}
	data, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", l.path, err)
	}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", l.path, err)
	}
	return l, nil
}

func (l *recordFile) find(path string) (entry, bool) {
	for _, e := range l.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return entry{}, false
}

func (l *recordFile) put(e entry) {
	l.drop(e.Path)
	l.Entries = append(l.Entries, e)
}

func (l *recordFile) drop(path string) {
	kept := l.Entries[:0]
	for _, e := range l.Entries {
		if e.Path != path {
			kept = append(kept, e)
		}
	}
	l.Entries = kept
}

func (l *recordFile) save() error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(l.path, append(data, '\n'), 0o600)
}

func status(o Options, agent string) string {
	h, ok := Lookup(agent)
	if !ok || h.Setup != SetupHooks {
		return StatusNoAgent
	}
	root, path, events := h.Root(o), h.Config(o), h.events
	if agent == Claude && o.Local {
		root, path = filepath.Join(o.Cwd, ".claude"), filepath.Join(o.Cwd, ".claude", "settings.local.json")
	}
	if !dirExists(root) {
		return StatusNoAgent
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StatusAbsent
	}
	have, err := ownedEvents(data, agent)
	if err != nil {
		return StatusAbsent
	}
	st := coverage(have, events)
	// Droid entries an earlier build nested under "hooks" never run; a
	// reinstall moves them, so they read as outdated rather than absent.
	if agent == Droid && st == StatusAbsent && legacyDroidHooks(data) {
		return StatusOutdated
	}
	return st
}

// coverage is installed when every event has a togo entry, outdated when
// only some do, and absent when none do.
func coverage(have map[string]bool, events []string) string {
	n := 0
	for _, ev := range events {
		if have[ev] {
			n++
		}
	}
	switch n {
	case len(events):
		return StatusInstalled
	case 0:
		return StatusAbsent
	}
	return StatusOutdated
}
