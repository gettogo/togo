package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Removing a session's record also drops the file its chat feed remembered
// following, so those notes do not pile up as sessions come and go.
func TestRemoveDropsRememberedFollowFile(t *testing.T) {
	home := t.TempDir()
	m := &Manifest{Path: filepath.Join(home, "sessions.json")}
	if err := m.Put(ManifestEntry{ID: "s_abcdefghij", Bib: "01", Name: "OMP", Agent: "omp", Cwd: home, Command: []string{"omp"}, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(home, "run")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	mine, other := filepath.Join(run, "follow-s_abcdefghij.json"), filepath.Join(run, "follow-s_keepmekeep.json")
	for _, p := range []string{mine, other} {
		if err := os.WriteFile(p, []byte(`{"path":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Remove("s_abcdefghij"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatalf("follow file kept after remove: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another session's follow file was removed: %v", err)
	}
}
