// Package fsx validates and lists directories named by remote clients.
package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrInvalidPath is wrapped by every rejection of a client-supplied path.
var ErrInvalidPath = errors.New("invalid path")

const maxEntries = 2000

// Entry is one listed subdirectory.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// ValidateDir expands a leading "~" or "~/" to the home directory and
// returns the cleaned absolute path, which must name an existing directory
// (possibly through a symlink). The path is not symlink-resolved, so the
// caller sees the folder it asked for.
func ValidateDir(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: contains a NUL byte", ErrInvalidPath)
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidPath, err)
		}
		p = home + p[1:]
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q is not absolute", ErrInvalidPath, p)
	}
	p = filepath.Clean(p)
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrInvalidPath, p)
	}
	return p, nil
}

// ListDir validates p as ValidateDir does and returns the cleaned path with
// its subdirectories, symlinks to directories included, sorted
// case-insensitively. Names starting with "." are left out unless
// showHidden. At most 2000 entries are returned. A directory that cannot be
// read, such as one without permission, also wraps ErrInvalidPath.
func ListDir(p string, showHidden bool) (string, []Entry, error) {
	dir, err := ValidateDir(p)
	if err != nil {
		return "", nil, err
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, fmt.Errorf("%w: list %s: %w", ErrInvalidPath, dir, err)
	}
	entries := []Entry{}
	for _, de := range des {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if isDir(dir, de) {
			entries = append(entries, Entry{Name: name, Dir: true})
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	return dir, entries, nil
}

func isDir(parent string, de os.DirEntry) bool {
	if de.IsDir() {
		return true
	}
	if de.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, de.Name()))
	return err == nil && info.IsDir()
}
