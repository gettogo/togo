package gitx_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
)

// Config-defined hooks (hook.<name>.command and .event) are a second hook
// mechanism that core.hooksPath and --no-verify do not cover.
func TestConfigHooksNeverRun(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	script, marker := markerScript(t)
	events := []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit",
		"post-index-change", "reference-transaction", "post-checkout", "post-rewrite"}
	for _, ev := range events {
		git(t, dir, "config", "--add", "hook.mark.event", ev)
	}
	git(t, dir, "config", "hook.mark.command", script+" config-hook")
	// Edge friendly names: empty, spaces, "=", dots, upper case.
	for _, name := range []string{"", "a b", "a=b", "a.b.c", "UPPER"} {
		git(t, dir, "config", "hook."+name+".command", script+" named-"+name)
		for _, ev := range []string{"post-index-change", "reference-transaction", "post-commit", "pre-commit"} {
			git(t, dir, "config", "--add", "hook."+name+".event", ev)
		}
	}
	// A hook brought in by an include is blanked the same way.
	inc := filepath.Join(t.TempDir(), "extra.gitconfig")
	write(t, filepath.Dir(inc), "extra.gitconfig",
		"[hook \"inc.name\"]\n\tcommand = "+script+" included\n\tevent = post-index-change\n\tevent = post-commit\n")
	git(t, dir, "config", "include.path", inc)

	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "new.txt", "n\n")
	statusOf(t, s)
	steps := []struct{ method, params string }{
		{"git.stage", pathsParams([]string{"README.md", "new.txt"}, nil)},
		{"git.unstage", pathsParams([]string{"new.txt"}, nil)},
		{"git.discard", pathsParams([]string{"new.txt"}, seenAll(t, s, "new.txt"))},
		{"git.commit", commitParams("Change", false, "")},
		{"git.diff", `{` + sid + `,"commit":"HEAD"}`},
		{"git.log", `{` + sid + `}`},
	}
	for _, st := range steps {
		if err := call(t, s, st.method, st.params, nil); err != nil {
			t.Fatalf("%s: %v", st.method, err)
		}
	}
	write(t, dir, "README.md", "third\n")
	if _, err := writeCall(t, s, "git.stage", []string{"README.md"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.discard", []string{"README.md"}, seenAll(t, s, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.commit", commitParams("Amended", true, head(t, dir)), nil); err != nil {
		t.Fatal(err)
	}
	statusOf(t, s)
	if exists(marker) {
		t.Fatalf("a config-defined hook ran: %s", read(t, marker))
	}

	// The control proves the hooks are live on this git: plain git runs them.
	cmd := exec.Command("git", "-C", dir, "-c", "core.hooksPath=/dev/null", "commit", "--no-verify", "--allow-empty", "-qm", "control")
	cmd.Env = gitx.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("control commit: %v\n%s", err, out)
	}
	if !exists(marker) {
		t.Skip("this git has no config-defined hooks")
	}
}
