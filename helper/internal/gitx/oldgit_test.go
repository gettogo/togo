package gitx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
)

// oldGit returns a git wrapper that, like git before 2.30, fails rev-parse
// when it is given --end-of-options.
func oldGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	p := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\nold=0\nfor a in \"$@\"; do\n" +
		"  case \"$a\" in --end-of-options) old=1;; rev-parse) rp=1;; esac\n" +
		"done\n" +
		"if [ \"$old\" = 1 ] && [ \"$rp\" = 1 ]; then exit 1; fi\n" +
		"exec " + real + " \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHistoryWorksOnGitWithoutEndOfOptions(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")
	s := service(dir)
	s.Git = gitx.Git{Bin: oldGit(t)}
	var lg struct{ Commits []gitx.Commit }
	if err := call(t, s, "git.log", `{`+sid+`}`, &lg); err != nil || len(lg.Commits) != 2 {
		t.Fatalf("log: %v, %d commits", err, len(lg.Commits))
	}
	var d gitx.Diff
	h := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	if err := call(t, s, "git.diff", `{`+sid+`,"commit":"`+h+`"}`, &d); err != nil || !strings.Contains(d.Diff, "a.txt") {
		t.Fatalf("diff: %v %q", err, d.Diff)
	}
}
