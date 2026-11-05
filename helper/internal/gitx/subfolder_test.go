package gitx_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
)

// monorepo returns a seeded repository with a committed apps/web/app.js and
// the session folder apps/web below its root.
func monorepo(t *testing.T) (root, cwd string) {
	t.Helper()
	root = seeded(t)
	write(t, root, "apps/web/app.js", "one\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "web")
	return root, filepath.Join(root, "apps", "web")
}

func TestSubfolderSessionUsesRootPaths(t *testing.T) {
	root, cwd := monorepo(t)
	s := writer(cwd)
	write(t, root, "apps/web/app.js", "two\n")
	write(t, root, "apps/web/new.js", "new\n")
	write(t, root, "README.md", "changed\n")

	st := statusOf(t, s)
	got := entries(st)
	for _, p := range []string{"apps/web/app.js", "apps/web/new.js", "README.md"} {
		if _, ok := got[p]; !ok {
			t.Fatalf("status has no %q: %+v", p, st.Files)
		}
	}

	var d gitx.Diff
	if err := call(t, s, "git.diff", `{`+sid+`,"path":"apps/web/app.js"}`, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Diff, "+two") {
		t.Fatalf("worktree diff of a root-relative path is empty: %q", d.Diff)
	}
	if err := call(t, s, "git.diff", `{`+sid+`,"path":"README.md"}`, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Diff, "+changed") {
		t.Fatalf("a changed file outside the session folder has no diff: %q", d.Diff)
	}

	if _, err := writeCall(t, s, "git.stage", []string{"apps/web/app.js"}, seenAll(t, s, "apps/web/app.js")); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.diff", `{`+sid+`,"path":"apps/web/app.js","staged":true}`, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Diff, "+two") {
		t.Fatalf("staged diff of a root-relative path is empty: %q", d.Diff)
	}

	var c gitx.CommitResult
	if err := call(t, s, "git.commit", commitParams("web two", false, head(t, root)), &c); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.diff", `{`+sid+`,"commit":"`+c.Hash+`","path":"apps/web/app.js"}`, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Diff, "+two") || !strings.Contains(d.Diff, "b/apps/web/app.js") {
		t.Fatalf("commit diff of a root-relative path: %q", d.Diff)
	}
}
