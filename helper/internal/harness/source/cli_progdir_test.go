package source

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
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

// A program that exits well but leaves a background child holding its
// output pipe still returns its output.
func TestRunKeepsOutputWhenChildHoldsPipe(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, "broker", "sleep 3 &\necho '[1,2]'\nexit 0\n")
	env := harness.Env{Cwd: dir, Vars: map[string]string{VarHome: dir, VarPath: dir + ":/usr/bin:/bin"}}
	out, err := Run(context.Background(), []string{"broker"}, env)
	if err != nil || string(bytes.TrimSpace(out)) != "[1,2]" {
		t.Fatalf("out %q err %v", out, err)
	}
}
