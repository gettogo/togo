package sessiongen

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/auggie"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kiro"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/openhands"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The Terminal profile writes nothing because the adapters it stands for
// read nothing: Auggie, Kiro CLI and OpenHands declare no capability, no
// feed and no source.
func TestTerminalAdaptersReadNothing(t *testing.T) {
	files := harness.AdapterFiles()
	for _, id := range []string{"auggie", "kiro", "openhands"} {
		b, ok := files[id]
		if !ok {
			t.Fatalf("no adapter file registered for %s", id)
		}
		a, err := spec.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Capabilities) != 0 || len(a.Feeds) != 0 || len(a.Sources) != 0 {
			t.Errorf("%s declares capabilities %v, feeds %d, sources %d: the profile must write what they read", id, a.Capabilities, len(a.Feeds), len(a.Sources))
		}
	}
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	_ = os.MkdirAll(cwd, 0o755)
	s := Script{Seed: 1, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd}
	if _, err := Write(home, Terminal{}, s); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 1 {
		t.Errorf("the profile wrote files: %v", ents)
	}
	s.Turns = []Turn{{Say: 10}}
	if _, err := Write(home, Terminal{}, s); err == nil {
		t.Error("a turn was written")
	}
}
