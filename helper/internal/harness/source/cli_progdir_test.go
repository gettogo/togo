package source

import (
	"slices"
	"testing"
)

// A script's interpreter beside it (npm's node in /opt/homebrew/bin) is
// found even before the login PATH names that folder.
func TestWithProgDirAddsTheProgramsFolder(t *testing.T) {
	got := withProgDir([]string{"HOME=/h", "PATH=/usr/bin:/bin"}, "/opt/homebrew/bin/auggie")
	if got[len(got)-1] != "PATH=/usr/bin:/bin:/opt/homebrew/bin" {
		t.Fatalf("got %v", got)
	}
	same := []string{"PATH=/opt/homebrew/bin:/usr/bin"}
	if out := withProgDir(same, "/opt/homebrew/bin/auggie"); !slices.Equal(out, same) {
		t.Fatalf("changed %v", out)
	}
}
