package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// watched are the harness config files a run could change: through Togo's
// own actions (a typed /model that the harness saves, a trust prompt the
// suite answers for its scratch folder). Files marked hot are rewritten by
// every running instance of the harness, so they are never restored whole:
// only entries naming the run's folders are removed from them.
var watched = map[string][]struct {
	path string
	hot  bool
}{
	"claude":   {{"~/.claude/settings.json", false}, {"~/.claude.json", true}},
	"codex":    {{"~/.codex/config.toml", false}},
	"opencode": {{"~/.config/opencode/opencode.json", false}, {"~/.local/state/opencode/model.json", false}},
	"kilo":     {{"~/.config/kilo/kilo.jsonc", false}, {"~/.local/state/kilo/model.json", false}},
	"gemini":   {{"~/.gemini/settings.json", false}, {"~/.gemini/trustedFolders.json", false}, {"~/.gemini/projects.json", true}},
	"omp":      {{"~/.omp/agent/config.yml", false}},
	"pi":       {{"~/.pi/agent/settings.json", false}},
	"kimi":     {{"~/.kimi-code/config.toml", false}, {"~/.kimi-code/tui.toml", false}, {"~/.kimi-code/workspaces.json", true}, {"~/.kimi-code/projects.json", true}},
	"copilot":  {{"~/.copilot/config.json", false}, {"~/.copilot/open-sessions-state.json", true}},
	"droid":    {{"~/.factory/settings.json", false}},
	"auggie":   {{"~/.augment/settings.json", false}, {"~/.augment/.auggie.json", false}},
	"vibe":     {{"~/.vibe/config.toml", false}, {"~/.vibe/trusted_folders.toml", false}},
	"crush":    {{"~/.config/crush/crush.json", false}, {"~/.local/share/crush/crush.json", false}},
	"goose":    {{"~/.config/goose/config.yaml", false}},
	"qwen":     {{"~/.qwen/settings.json", false}},
	"aider":    {{"~/.aider.conf.yml", false}},
}

// artifactRoots hold per-folder records a harness makes for a new folder;
// entries whose name carries the run id are the run's own and are removed.
var artifactRoots = []string{
	"~/.claude/projects", "~/.omp/agent/sessions", "~/.pi/agent/sessions", "~/.qwen/projects",
	"~/.kimi-code/workspace-trust", "~/.gemini/history",
}

type snapEntry struct {
	Harness string    `json:"harness"`
	Path    string    `json:"path"`
	Hot     bool      `json:"hot"`
	Existed bool      `json:"existed"`
	Sum     string    `json:"sum"`
	Copy    string    `json:"copy"`
	At      time.Time `json:"at"`
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// snapshot copies every watched file of the harnesses into dir (0600) and
// writes the manifest there, so -cleanup can restore after a crash.
func snapshot(dir string, harnesses []string) ([]snapEntry, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var out []snapEntry
	for _, h := range harnesses {
		for i, w := range watched[h] {
			p := expand(w.path)
			e := snapEntry{Harness: h, Path: p, Hot: w.hot, At: time.Now()}
			if b, err := os.ReadFile(p); err == nil {
				e.Existed, e.Sum = true, sum(b)
				e.Copy = filepath.Join(dir, fmt.Sprintf("%s-%d", h, i))
				if err := os.WriteFile(e.Copy, b, 0o600); err != nil {
					return nil, err
				}
			}
			out = append(out, e)
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return out, os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600)
}

func loadSnapshot(dir string) []snapEntry {
	var out []snapEntry
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// restore puts every changed watched file back and returns one line per
// file it touched or could not. marker is the run id: entries naming it
// are scrubbed from hot files.
func restore(entries []snapEntry, marker string) []string {
	var notes []string
	for _, e := range entries {
		cur, err := os.ReadFile(e.Path)
		switch {
		case err != nil && !e.Existed:
			continue
		case err != nil:
			notes = append(notes, fmt.Sprintf("%s: removed during the run; not recreated (check by hand)", short(e.Path)))
			continue
		case !e.Existed:
			if bytes.Contains(cur, []byte(marker)) {
				_ = os.Remove(e.Path)
				notes = append(notes, fmt.Sprintf("%s: created by the run; removed", short(e.Path)))
			} else {
				notes = append(notes, fmt.Sprintf("%s: created during the run; left in place", short(e.Path)))
			}
			continue
		case sum(cur) == e.Sum:
			continue
		}
		if e.Hot {
			if keys := changedKeys(e.Copy, cur, marker); len(keys) > 0 {
				notes = append(notes, fmt.Sprintf("%s: top-level keys changed during the run (not restored: other instances write it): %s", short(e.Path), strings.Join(keys, ", ")))
			}
			if !bytes.Contains(cur, []byte(marker)) {
				continue // changed by the harness's other instances, not by us
			}
			out, n, err := scrubJSON(cur, marker)
			if err != nil || n == 0 {
				notes = append(notes, fmt.Sprintf("%s: names the run's folder but could not be scrubbed: %v", short(e.Path), err))
				continue
			}
			if err := writeAtomic(e.Path, out); err != nil {
				notes = append(notes, fmt.Sprintf("%s: scrub failed: %v", short(e.Path), err))
				continue
			}
			notes = append(notes, fmt.Sprintf("%s: removed %d entr(ies) naming the run's folders", short(e.Path), n))
			continue
		}
		if mine, why := owned.ownChange(e.Path, sum(cur)); !mine {
			notes = append(notes, fmt.Sprintf("left %s: changed outside the suite (%s); not restored", short(e.Path), why))
			continue
		}
		orig, err := os.ReadFile(e.Copy)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: changed, snapshot unreadable: %v", short(e.Path), err))
			continue
		}
		bk, err := backup(e.Path, cur)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: backup before restore failed (%v); not restored", short(e.Path), err))
			continue
		}
		notes = append(notes, fmt.Sprintf("%s: current copy saved to %s", short(e.Path), short(bk)))
		if err := writeAtomic(e.Path, orig); err != nil {
			notes = append(notes, fmt.Sprintf("%s: restore failed: %v", short(e.Path), err))
			continue
		}
		if b, _ := os.ReadFile(e.Path); sum(b) == e.Sum {
			notes = append(notes, fmt.Sprintf("%s: changed during the run; restored byte for byte", short(e.Path)))
		} else {
			notes = append(notes, fmt.Sprintf("%s: restore did not verify", short(e.Path)))
		}
	}
	return notes
}

func writeAtomic(p string, b []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(p); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := p + ".togo-conformance.tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func short(p string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(p, home+"/") {
		return "~/" + p[len(home)+1:]
	}
	return p
}

// removeArtifacts deletes the per-folder records harnesses made for the
// run's folders: entries in artifactRoots whose name carries marker.
func removeArtifacts(marker string) []string {
	var notes []string
	for _, root := range artifactRoots {
		entries, err := os.ReadDir(expand(root))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), marker) {
				p := filepath.Join(expand(root), e.Name())
				if os.RemoveAll(p) == nil {
					notes = append(notes, "removed "+short(p))
				}
			}
		}
	}
	return notes
}

// changedKeys names the top-level keys of a JSON file whose values differ
// from the snapshot, leaving out keys that name the run.
func changedKeys(snapCopy string, cur []byte, marker string) []string {
	old, err := os.ReadFile(snapCopy)
	if err != nil {
		return nil
	}
	var a, b map[string]json.RawMessage
	if json.Unmarshal(old, &a) != nil || json.Unmarshal(cur, &b) != nil {
		return nil
	}
	var out []string
	for k, v := range b {
		if strings.Contains(k, marker) {
			continue
		}
		if w, ok := a[k]; !ok || !bytes.Equal(v, w) {
			out = append(out, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k+" (removed)")
		}
	}
	sort.Strings(out)
	return out
}

// runFileRoots hold per-session files harnesses write, named by their own
// ids; the run's are the ones written during it that name its folders.
var runFileRoots = []string{
	"~/.copilot/sidebar-sessions-state", "~/.copilot/session-state", "~/.gemini/tmp",
	"~/.codex/sessions", "~/.kimi-code/sessions", "~/.factory/sessions", "~/.vibe/logs",
	"~/.augment/sessions", "~/.cline/data/sessions",
}

// removeRunFiles deletes files under runFileRoots modified since since
// that mention marker, then the folders that left empty.
func removeRunFiles(marker string, since time.Time) []string {
	var notes []string
	for _, root := range runFileRoots {
		root = expand(root)
		var dirs []string
		n := 0
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.Count(strings.TrimPrefix(p, root), "/") > 5 {
					return filepath.SkipDir
				}
				if p != root {
					dirs = append(dirs, p)
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(since) || info.Size() > 64<<20 {
				return nil
			}
			if strings.Contains(p, marker) || fileContains(p, marker) {
				if os.Remove(p) == nil {
					n++
				}
			}
			return nil
		})
		for i := len(dirs) - 1; i >= 0; i-- {
			if info, err := os.Stat(dirs[i]); err == nil && !info.ModTime().Before(since) {
				_ = os.Remove(dirs[i]) // only succeeds when empty
			}
		}
		if n > 0 {
			notes = append(notes, fmt.Sprintf("removed %d session file(s) of the run under %s", n, short(root)))
		}
	}
	return notes
}
