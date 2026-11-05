package gitx_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/wire"
)

func commitParams(message string, amend bool, head string) string {
	p := map[string]any{"id": "s_abcdefghij", "message": message, "amend": amend}
	if head != "" {
		p["head"] = head
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func head(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
}

func TestCommitMessageFromStdin(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "add", "README.md")
	msg := "-n \"quoted\" 'single' $(touch pwned) `touch pwned2`\n\nBody line\twith a tab.\n--amend"
	var res gitx.CommitResult
	if err := call(t, s, "git.commit", commitParams(msg, false, ""), &res); err != nil {
		t.Fatal(err)
	}
	if res.Hash != head(t, dir) || !strings.HasPrefix(res.Hash, res.Short) || len(res.Short) < 7 {
		t.Errorf("result %+v, HEAD %s", res, head(t, dir))
	}
	if got := strings.TrimRight(git(t, dir, "log", "-1", "--format=%B"), "\n"); got != msg {
		t.Errorf("message %q, want %q", got, msg)
	}
	if exists(filepath.Join(dir, "pwned")) || exists(filepath.Join(dir, "pwned2")) {
		t.Error("message was run by a shell")
	}
	if strings.TrimSpace(git(t, dir, "rev-list", "--count", "HEAD")) != "2" {
		t.Error("expected exactly one new commit")
	}
}

func TestCommitHeadChecked(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	old := head(t, dir)
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "commit", "-qam", "moved")
	write(t, dir, "README.md", "third\n")
	git(t, dir, "add", "README.md")
	if err := call(t, s, "git.commit", commitParams("m", false, old), nil); conflictReason(err) != wire.ConflictStale {
		t.Errorf("commit with old head: %v", err)
	}
	if err := call(t, s, "git.commit", commitParams("m", true, old), nil); conflictReason(err) != wire.ConflictStale {
		t.Errorf("amend with old head: %v", err)
	}
	if strings.TrimSpace(git(t, dir, "rev-list", "--count", "HEAD")) != "2" {
		t.Fatal("a commit was made")
	}
	var res gitx.CommitResult
	if err := call(t, s, "git.commit", commitParams("Amended", true, head(t, dir)), &res); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(git(t, dir, "rev-list", "--count", "HEAD")) != "2" || res.Hash != head(t, dir) {
		t.Error("amend did not replace HEAD")
	}
	if got := strings.TrimSpace(git(t, dir, "log", "-1", "--format=%s")); got != "Amended" {
		t.Errorf("subject %q", got)
	}
}

func TestCommitNothingStaged(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "unstaged only\n")
	if err := call(t, s, "git.commit", commitParams("Empty", false, ""), nil); conflictReason(err) != wire.ConflictNothingStaged {
		t.Errorf("got %v", err)
	}
}

func TestCommitIdentityMissing(t *testing.T) {
	isolate(t)
	dir := newRepo(t)
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	t.Setenv("EMAIL", "someone@example.com")
	err := call(t, writer(dir), "git.commit", commitParams("First", false, ""), nil)
	if r := conflictReason(err); r != wire.ConflictIdentity {
		t.Errorf("reason %q err %v", r, err)
	}
}

func TestAmendUnborn(t *testing.T) {
	isolate(t)
	dir := newRepo(t)
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "config", "user.email", "t@example.com")
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	err := call(t, writer(dir), "git.commit", commitParams("x", true, strings.Repeat("0", 40)), nil)
	if r := conflictReason(err); r != wire.ConflictUnbornAmend {
		t.Errorf("reason %q err %v", r, err)
	}
	// The first commit of a repository works.
	if err := call(t, writer(dir), "git.commit", commitParams("First", false, ""), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCommitParamsValidated(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "staged\n")
	git(t, dir, "add", "README.md")
	h := head(t, dir)
	for name, params := range map[string]string{
		"amend without head": commitParams("m", true, ""),
		"short head":         commitParams("m", false, h[:12]),
		"upper head":         commitParams("m", true, strings.ToUpper(h)),
		"empty message":      commitParams("", false, ""),
		"blank first line":   commitParams("  \nbody", false, ""),
		"control character":  commitParams("a\x1bb", false, ""),
		"carriage return":    commitParams("a\r\nb", false, ""),
		"c1 control":         commitParams("a\u0085b", false, ""),
		"too long":           commitParams(strings.Repeat("a", wire.MaxCommitBytes+1), false, ""),
		"invalid utf-8":      `{` + sid + `,"message":"a\xffb","amend":false}`,
	} {
		if err := call(t, s, "git.commit", params, nil); !isInvalidParams(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if strings.TrimSpace(git(t, dir, "rev-list", "--count", "HEAD")) != "1" {
		t.Error("a commit was made")
	}
}

func TestWritesNeverRunRepoPrograms(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	script, marker := markerScript(t)
	hooks := t.TempDir()
	names := []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-checkout",
		"post-index-change", "reference-transaction", "post-rewrite", "pre-auto-gc"}
	for _, h := range names {
		for _, d := range []string{hooks, filepath.Join(dir, ".git", "hooks")} {
			write(t, d, h, "#!/bin/sh\n'"+script+"' hook\nexit 1\n")
			if err := os.Chmod(filepath.Join(d, h), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kv := range [][2]string{
		{"core.hooksPath", hooks},
		{"core.fsmonitor", script},
		{"core.editor", script},
		{"gpg.program", script},
		{"gpg.ssh.program", script},
		{"commit.gpgSign", "true"},
		{"diff.external", script},
		{"gc.auto", "1"},
	} {
		git(t, dir, "config", kv[0], kv[1])
	}
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "new.txt", "n\n")
	if _, err := writeCall(t, s, "git.stage", []string{"README.md"}, seenAll(t, s, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.discard", []string{"new.txt"}, seenAll(t, s, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.commit", commitParams("Change", false, ""), nil); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "README.md", "third\n")
	if _, err := writeCall(t, s, "git.stage", []string{"README.md"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.unstage", []string{"README.md"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.discard", []string{"README.md"}, seenAll(t, s, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := call(t, s, "git.commit", commitParams("Amended", true, head(t, dir)), nil); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatalf("a repository program ran: %s", read(t, marker))
	}
	if strings.Contains(git(t, dir, "cat-file", "commit", "HEAD"), "gpgsig") {
		t.Error("commit was signed")
	}
	// The control proves the hooks are live: plain git commit runs them.
	cmd := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-qm", "control")
	cmd.Env = gitx.Env()
	_ = cmd.Run()
	if !exists(marker) {
		t.Error("control: plain git commit ran no hook")
	}
}

func TestDiffCommitAgainstParent(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "commit", "-qam", "second")
	write(t, dir, "README.md", "uncommitted\n")
	var d gitx.Diff
	if err := call(t, writer(dir), "git.diff", `{`+sid+`,"commit":"HEAD"}`, &d); err != nil {
		t.Fatal(err)
	}
	want := "diff --git a/README.md b/README.md\nindex ce01362..13ab7f7 100644\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-hello\n+hello again\n"
	if d.Diff != want || d.Truncated || len(d.Binary) != 0 {
		t.Errorf("diff %+v", d)
	}
}

func TestDiffRootCommit(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "commit", "-qam", "second")
	var d gitx.Diff
	if err := call(t, writer(dir), "git.diff", `{`+sid+`,"commit":"HEAD~1"}`, &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Diff, "new file mode 100644") || !strings.Contains(d.Diff, "+hello\n") {
		t.Errorf("diff %q", d.Diff)
	}
}

func TestDiffCommitWithStagedRejected(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	for _, params := range []string{
		`{` + sid + `,"commit":"HEAD","staged":true}`,
		`{` + sid + `,"commit":"HEAD","ref":"HEAD"}`,
	} {
		if err := call(t, s, "git.diff", params, nil); !isInvalidParams(err) {
			t.Errorf("%s: %v", params, err)
		}
	}
	for _, c := range []string{"-p", "no-such-commit"} {
		if err := call(t, s, "git.diff", `{`+sid+`,"commit":`+quote(c)+`}`, nil); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("commit %q: %v", c, err)
		}
	}
}

func TestCommitReportsRacedHead(t *testing.T) {
	dir := seeded(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// Another process commits just as the helper's git commit starts.
	g := "'" + real + "' -C '" + dir + "' -c user.name=r -c user.email=r@example.com -c core.hooksPath=/dev/null "
	racer := "h=$(" + g + "commit-tree 'HEAD^{tree}' -p HEAD -m racer) && " + g + "update-ref HEAD \"$h\""
	s := &gitx.Service{Sessions: writeSessions{cwd: dir}, Git: wrappedGit(t, "commit", racer)}
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "add", "README.md")
	seenHead := head(t, dir)
	err = call(t, s, "git.commit", commitParams("Mine", false, seenHead), nil)
	var ce *api.CodeError
	if conflictReason(err) != wire.ConflictRaced || !errors.As(err, &ce) {
		t.Fatalf("got %v", err)
	}
	if data, _ := ce.Data.(map[string]any); data["hash"] != head(t, dir) {
		t.Errorf("hash %v, HEAD %s", data["hash"], head(t, dir))
	}
	if got := strings.TrimSpace(git(t, dir, "log", "--format=%s", "-3")); got != "Mine\nracer\ninit" {
		t.Errorf("history rewritten: %q", got)
	}

	write(t, dir, "README.md", "third\n")
	git(t, dir, "add", "README.md")
	err = call(t, s, "git.commit", commitParams("Amended", true, head(t, dir)), nil)
	if conflictReason(err) != wire.ConflictRaced {
		t.Fatalf("amend: %v", err)
	}
	// Without a race the same checks pass.
	write(t, dir, "README.md", "fourth\n")
	git(t, dir, "add", "README.md")
	if err := call(t, writer(dir), "git.commit", commitParams("Calm", false, head(t, dir)), nil); err != nil {
		t.Fatal(err)
	}
	if err := call(t, writer(dir), "git.commit", commitParams("Calm again", true, head(t, dir)), nil); err != nil {
		t.Fatal(err)
	}
}
