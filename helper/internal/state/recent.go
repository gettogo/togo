package state

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
)

const defaultRecentMax = 20

// Recent is the most-recently-used list of session folders, newest first.
type Recent struct {
	// File is the JSON file holding {"dirs": [...]}.
	File string
	// Max caps the list; zero means 20.
	Max int

	mu sync.Mutex
}

type recentFile struct {
	Dirs []string `json:"dirs"`
}

// Add moves dir to the front of the list, dropping any older copy and
// trimming the list to Max.
func (r *Recent) Add(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dirs := slices.DeleteFunc(r.read(), func(d string) bool { return d == dir })
	dirs = append([]string{dir}, dirs...)
	limit := r.Max
	if limit <= 0 {
		limit = defaultRecentMax
	}
	if len(dirs) > limit {
		dirs = dirs[:limit]
	}
	data, err := json.Marshal(recentFile{Dirs: dirs})
	if err != nil {
		return fmt.Errorf("recent dirs: %w", err)
	}
	return WriteFile(r.File, append(data, '\n'))
}

// List returns the recorded folders, newest first. A missing, unreadable or
// corrupt file is an empty list; the next Add replaces it.
func (r *Recent) List() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read(), nil
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
