package gitx_test

import (
	"path/filepath"
	"testing"
)

func TestUnstageRenameRestoresOldPathInIndex(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "mv", "README.md", "NEW.md")
	if _, err := writeCall(t, s, "git.unstage", []string{"NEW.md"}, seenAll(t, s, "NEW.md")); err != nil {
		t.Fatal(err)
	}
	if got := letters(t, s, "README.md"); got != ".D" {
		t.Errorf("README.md is %q after unstaging the rename, want .D", got)
	}
	if got := letters(t, s, "NEW.md"); got != "??" {
		t.Errorf("NEW.md is %q after unstaging the rename, want ??", got)
	}
}

func TestDiscardNeedsSeenForEveryPath(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "edited\n")
	write(t, dir, "other.txt", "x\n")
	if _, err := writeCall(t, s, "git.discard", []string{"README.md"}, nil); !isInvalidParams(err) {
		t.Errorf("discard without seen: %v, want invalid params", err)
	}
	seen := seenAll(t, s, "README.md")
	if _, err := writeCall(t, s, "git.discard", []string{"README.md", "other.txt"}, seen); !isInvalidParams(err) {
		t.Errorf("discard with a path missing from seen: %v, want invalid params", err)
	}
	if got := read(t, filepath.Join(dir, "README.md")); got != "edited\n" {
		t.Errorf("README.md changed to %q", got)
	}
}
