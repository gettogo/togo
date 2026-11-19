package sessiongen

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/adapters/auggie"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The Auggie profile writes nothing because its adapter reads nothing:
// auggie.yaml declares no capability, no feed and no source.
func TestAuggieAdapterReadsNothing(t *testing.T) {
	a, err := spec.Parse(auggie.AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Capabilities) != 0 || len(a.Feeds) != 0 || len(a.Sources) != 0 {
		t.Fatalf("auggie.yaml declares capabilities %v, feeds %d, sources %d: the profile must write what they read", a.Capabilities, len(a.Feeds), len(a.Sources))
	}
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	_ = os.MkdirAll(cwd, 0o755)
	s := Script{Seed: 1, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd}
	if _, err := Write(home, Auggie{}, s); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 1 {
		t.Errorf("the profile wrote files: %v", ents)
	}
	s.Turns = []Turn{{Say: 10}}
	if _, err := Write(home, Auggie{}, s); err == nil {
		t.Error("a turn was written")
	}
}
