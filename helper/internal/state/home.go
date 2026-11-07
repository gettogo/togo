// Package state owns the helper's files under $TOGO_HOME and facts about the
// host user's environment that are costly to compute.
package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home returns $TOGO_HOME, or ~/.togo when unset, creating it with mode 0700
// and tightening an existing directory to 0700.
func Home() (string, error) {
	dir := os.Getenv("TOGO_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("togo home: %w", err)
		}
		dir = filepath.Join(home, ".togo")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("togo home: %w", err)
	}
	return dir, nil
}

// WriteFile replaces path with data, mode 0600. It writes a temporary file in
// the same directory, syncs it and renames it, so readers never see a
// partial file, even after a power loss.
func WriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
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
