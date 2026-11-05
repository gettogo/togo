package gitx_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
)

// hostileSubmodule adds a submodule at sub whose HEAD has moved past the
// recorded commit, whose working tree is dirty and stat-dirty, and whose
// own config names a program for every way git could run one inside it.
func hostileSubmodule(t *testing.T) (dir, marker string) {
	t.Helper()
	dir = seeded(t)
	src := newRepo(t)
	write(t, src, "a.txt", "a\n")
	git(t, src, "add", "a.txt")
	git(t, src, "commit", "-qm", "sub init")
	git(t, dir, "submodule", "add", "-q", src, "sub")
	git(t, dir, "commit", "-qm", "add sub")
	sub := filepath.Join(dir, "sub")
	write(t, sub, "b.txt", "b\n")
	git(t, sub, "add", "b.txt")
	git(t, sub, "commit", "-qm", "sub second")

	script, marker := markerScript(t)
	hooks := t.TempDir()
	write(t, hooks, "post-index-change", "#!/bin/sh\n'"+script+"' sub-hook\n")
	if err := os.Chmod(filepath.Join(hooks, "post-index-change"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, sub, ".gitattributes", "*.txt filter=m diff=m\n")
	for _, kv := range [][2]string{
		{"filter.m.clean", script + " sub-clean"},
		{"filter.m.smudge", script + " sub-smudge"},
		{"diff.m.textconv", script + " sub-textconv"},
		{"core.fsmonitor", script},
		{"core.hooksPath", hooks},
		{"hook.s.command", script + " sub-config-hook"},
		{"hook.s.event", "post-index-change"},
	} {
		git(t, sub, "config", kv[0], kv[1])
	}
	write(t, sub, "b.txt", "b, edited\n")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(sub, "a.txt"), later, later); err != nil {
		t.Fatal(err)
	}
	return dir, marker
}

func TestSubmodulesNeverEntered(t *testing.T) {
	dir, marker := hostileSubmodule(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "scratch.txt", "s\n")

	got := entries(statusOf(t, s))
	if f, ok := got["sub"]; !ok || f.Index+f.Worktree != ".M" {
		t.Errorf("gitlink change: %+v", got["sub"])
	}
	for p := range got {
		if strings.HasPrefix(p, "sub/") {
			t.Errorf("status entered the submodule: %q", p)
		}
	}
	for _, params := range []string{
		`{` + sid + `}`, `{` + sid + `,"staged":true}`, `{` + sid + `,"ref":"HEAD"}`, `{` + sid + `,"commit":"HEAD"}`,
	} {
		if err := call(t, s, "git.diff", params, nil); err != nil {
			t.Fatalf("diff %s: %v", params, err)
		}
	}
	if err := call(t, s, "git.log", `{`+sid+`}`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.stage", []string{"sub/b.txt"}, nil); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("path inside the submodule: %v", err)
	}
	if _, err := writeCall(t, s, "git.stage", []string{"sub"}, nil); !isInvalidParams(err) {
		t.Errorf("the submodule itself: %v", err)
	}
	if _, err := writeCall(t, s, "git.stage", []string{"README.md"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.discard", []string{"scratch.txt"}, seenAll(t, s, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.commit", commitParams("Change", false, ""), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.unstage", []string{"README.md"}, nil); conflictReason(err) == "" {
		t.Errorf("unstage after commit: %v", err)
	}
	statusOf(t, s)
	if exists(marker) {
		t.Fatalf("a submodule program ran: %s", read(t, marker))
	}

	// The control proves the submodule config is live: plain git status
	// enters the submodule and runs its programs.
	cmd := exec.Command("git", "-C", dir, "status")
	cmd.Env = gitx.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !exists(marker) {
		t.Error("control: plain git status ran nothing in the submodule")
	}
}

func TestDiffPrefixesPinned(t *testing.T) {
	dir := seeded(t)
	for _, kv := range [][2]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.srcPrefix", "x/"},
		{"diff.dstPrefix", "y/"},
		{"diff.relative", "true"},
		{"diff.submodule", "diff"},
	} {
		git(t, dir, "config", kv[0], kv[1])
	}
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-qm", "second")
	write(t, dir, "README.md", "third\n")
	git(t, dir, "add", "README.md")
	write(t, dir, "README.md", "fourth\n")
	s := writer(dir)
	for _, params := range []string{
		`{` + sid + `}`, `{` + sid + `,"staged":true}`, `{` + sid + `,"ref":"HEAD"}`, `{` + sid + `,"commit":"HEAD"}`,
	} {
		var d gitx.Diff
		if err := call(t, s, "git.diff", params, &d); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(d.Diff, "diff --git a/README.md b/README.md\n") ||
			!strings.Contains(d.Diff, "\n--- a/README.md\n+++ b/README.md\n") {
			t.Errorf("%s: %q", params, d.Diff)
		}
	}
}
