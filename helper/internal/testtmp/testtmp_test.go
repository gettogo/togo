package testtmp

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A run folder whose process is gone is removed; one whose process is
// alive, and other folders, are not.
func TestSweepRemovesOnlyDeadRuns(t *testing.T) {
	dir := t.TempDir()
	dead := filepath.Join(dir, prefix+"999999999-abc")
	live := filepath.Join(dir, prefix+strconv.Itoa(os.Getpid())+"-abc")
	other := filepath.Join(dir, "TestSomething123")
	for _, d := range []string{dead, live, other} {
		if err := os.MkdirAll(filepath.Join(d, "001"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sweep(dir)
	for d, want := range map[string]bool{dead: false, live: true, other: true} {
		if _, err := os.Stat(d); (err == nil) != want {
			t.Errorf("%s: exists %v, want %v", filepath.Base(d), err == nil, want)
		}
	}
}
