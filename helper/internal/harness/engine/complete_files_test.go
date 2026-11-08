package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The @ menu never offers the litter an OS leaves in a folder, and lists the
// rest shortest path first, alphabetically within a length.
func TestCompleteFilesHidesJunkAndOrdersStably(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".DS_Store", "._Makefile", "Thumbs.db", "desktop.ini", "Makefile", "LICENSE", "README", "NOTICE", "web/.DS_Store", "web/LICENSE"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	items := completeFiles(context.Background(), dir, "", 50)
	var got []string
	for _, it := range items {
		got = append(got, it.Insert)
	}
	want := []string{"@NOTICE ", "@README ", "@LICENSE ", "@Makefile ", "@web/LICENSE "}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestIsJunk(t *testing.T) {
	for name, junk := range map[string]bool{".DS_Store": true, "._x.go": true, "Thumbs.db": true, ".gitignore": false, "Makefile": false} {
		if isJunk(name) != junk {
			t.Errorf("isJunk(%q) = %v", name, !junk)
		}
	}
}
