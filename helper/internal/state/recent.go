package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// defaultRecentMax keeps the list short enough to read at a glance: the
// few folders someone actually returns to.
const defaultRecentMax = 5

// Recent is the most-recently-used list of session folders, newest first.
type Recent struct {
	// File is the JSON file holding {"dirs": [...]}.
	File string
	// Max caps the list; zero means 5.
	Max int
	// Keeps reports whether dir belongs in the list. Nil means
	// WorthRemembering: no temporary folders, and only folders that exist.
	Keeps func(dir string) bool

	mu sync.Mutex
}

type recentFile struct {
	Dirs []string `json:"dirs"`
}

// Add moves dir to the front of the list, dropping any older copy, any
// folder no longer worth listing, and trimming the list to Max. A folder
// not worth remembering, such as a temporary one, is not recorded.
func (r *Recent) Add(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.keeps(dir) {
		return nil
	}
	dirs := slices.DeleteFunc(r.listed(), func(d string) bool { return d == dir })
	return r.write(append([]string{dir}, dirs...))
}

// Forget takes dir off the list. A folder that is not listed is no error.
func (r *Recent) Forget(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dirs := r.read()
	kept := slices.DeleteFunc(slices.Clone(dirs), func(d string) bool { return d == dir })
	if len(kept) == len(dirs) {
		return nil
	}
	return r.write(kept)
}

// List returns the recorded folders worth listing, newest first, at most
// Max. A missing, unreadable or corrupt file is an empty list; the next Add
// replaces it.
func (r *Recent) List() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trim(r.listed()), nil
}

func (r *Recent) write(dirs []string) error {
	data, err := json.Marshal(recentFile{Dirs: r.trim(dirs)})
	if err != nil {
		return fmt.Errorf("recent dirs: %w", err)
	}
	return WriteFile(r.File, append(data, '\n'))
}

func (r *Recent) trim(dirs []string) []string {
	limit := r.Max
	if limit <= 0 {
		limit = defaultRecentMax
	}
	if len(dirs) > limit {
		return dirs[:limit]
	}
	return dirs
}

func (r *Recent) keeps(dir string) bool {
	if r.Keeps != nil {
		return r.Keeps(dir)
	}
	return WorthRemembering(dir)
}

// listed is the stored list without the folders no longer worth listing.
func (r *Recent) listed() []string {
	return slices.DeleteFunc(r.read(), func(d string) bool { return !r.keeps(d) })
}

// read never fails: the list is a convenience, and a damaged file must not
// block session creation, which records every folder it uses.
func (r *Recent) read() []string {
	data, err := os.ReadFile(r.File)
	if err != nil {
		return []string{}
	}
	var f recentFile
	if err := json.Unmarshal(data, &f); err != nil || f.Dirs == nil {
		return []string{}
	}
	return f.Dirs
}

// WorthRemembering reports whether dir is a folder someone would come back
// to: it exists, and it is not a temporary folder.
func WorthRemembering(dir string) bool {
	home, _ := os.UserHomeDir()
	if IsTempDir(dir, home, os.TempDir()) {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// IsTempDir reports whether dir is a throwaway folder: under /tmp,
// /private/tmp, /var/folders or tmp (the user's $TMPDIR), or anywhere inside
// a folder named "scratchpad". A folder in home is the user's own even when
// home itself sits in a temporary folder, as it does in tests.
func IsTempDir(dir, home, tmp string) bool {
	clean := filepath.Clean(dir)
	if slices.Contains(strings.Split(clean, string(filepath.Separator)), "scratchpad") {
		return true
	}
	if home != "" && within(clean, filepath.Clean(home)) {
		return false
	}
	roots := []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders"}
	if tmp != "" {
		roots = append(roots, filepath.Clean(tmp))
		if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
			roots = append(roots, filepath.Clean(resolved))
		}
	}
	return slices.ContainsFunc(roots, func(root string) bool { return within(clean, root) })
}

func within(path, root string) bool {
	if root == "/" || root == "." {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
