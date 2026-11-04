package gitx_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/session"
)

type fakeSessions struct{ cwd string }

func (f fakeSessions) Resolve(context.Context, string) (session.Session, error) {
	return session.Session{ID: "s_abcdefghij", Cwd: f.cwd}, nil
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Env = append(gitx.Env(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func service(dir string) *gitx.Service {
	return &gitx.Service{Sessions: fakeSessions{cwd: dir}}
}

func call(t *testing.T, s *gitx.Service, method, params string, out any) error {
	t.Helper()
	res, err := s.Methods()[method](context.Background(), json.RawMessage(params))
	if err != nil {
		return err
	}
	b, merr := json.Marshal(res)
	if merr != nil {
		t.Fatal(merr)
	}
	if out != nil {
		if uerr := json.Unmarshal(b, out); uerr != nil {
			t.Fatal(uerr)
		}
	}
	return nil
}

const sid = `"id":"s_abcdefghij"`

func TestStatusPorcelainParsed(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "staged.txt", "1\n")
	write(t, dir, "unstaged.txt", "1\n")
	write(t, dir, "old name.txt", "same content here\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	write(t, dir, "staged.txt", "2\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "unstaged.txt", "2\n")
	git(t, dir, "mv", "old name.txt", "new name.txt")
	write(t, dir, "untracked.txt", "u\n")

	var st gitx.Status
	if err := call(t, service(dir), "git.status", `{`+sid+`}`, &st); err != nil {
		t.Fatal(err)
	}
	if !st.Repo || st.Root == nil || *st.Root != dir || st.Branch == nil || *st.Branch != "main" {
		t.Fatalf("header %+v", st)
	}
	got := map[string]gitx.FileStatus{}
	for _, f := range st.Files {
		got[f.Path] = f
	}
	check := func(path, index, worktree string) {
		t.Helper()
		if f, ok := got[path]; !ok || f.Index != index || f.Worktree != worktree {
			t.Errorf("%s: got %+v, want %s%s", path, f, index, worktree)
		}
	}
	check("staged.txt", "M", ".")
	check("unstaged.txt", ".", "M")
	check("untracked.txt", "?", "?")
	check("new name.txt", "R", ".")
	if r := got["new name.txt"]; r.OrigPath == nil || *r.OrigPath != "old name.txt" {
		t.Errorf("rename origPath %+v", r.OrigPath)
	}
}

func TestNotARepo(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s := service(dir)
	var st gitx.Status
	if err := call(t, s, "git.status", `{`+sid+`}`, &st); err != nil {
		t.Fatal(err)
	}
	if st.Repo || st.Root != nil || st.Branch != nil || len(st.Files) != 0 {
		t.Errorf("status %+v", st)
	}
	var br gitx.Branches
	if err := call(t, s, "git.branches", `{`+sid+`}`, &br); err != nil || br.Current != nil || len(br.Branches) != 0 {
		t.Errorf("branches %+v err %v", br, err)
	}
	var lg struct{ Commits []gitx.Commit }
	if err := call(t, s, "git.log", `{`+sid+`}`, &lg); err != nil || len(lg.Commits) != 0 {
		t.Errorf("log %+v err %v", lg, err)
	}
}

func TestDiffTruncatesAt512KiB(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "big.txt", "a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	write(t, dir, "big.txt", strings.Repeat("line of changed text\n", 60000))
	var d gitx.Diff
	if err := call(t, service(dir), "git.diff", `{`+sid+`}`, &d); err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || len(d.Diff) != gitx.MaxOutput {
		t.Errorf("truncated %v, len %d", d.Truncated, len(d.Diff))
	}
}

func TestDiffStagedAndRef(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "f.txt", "one\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "first")
	write(t, dir, "f.txt", "two\n")
	git(t, dir, "add", "f.txt")
	var d gitx.Diff
	if err := call(t, service(dir), "git.diff", `{`+sid+`,"staged":true,"path":"f.txt"}`, &d); err != nil || !strings.Contains(d.Diff, "+two") {
		t.Errorf("staged diff %q err %v", d.Diff, err)
	}
	if err := call(t, service(dir), "git.diff", `{`+sid+`,"ref":"HEAD"}`, &d); err != nil || !strings.Contains(d.Diff, "-one") {
		t.Errorf("ref diff %q err %v", d.Diff, err)
	}
}

func TestDiffListsBinaryFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "img.bin", "\x00\x01\x02")
	write(t, dir, "t.txt", "a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	write(t, dir, "img.bin", "\x00\x01\x02\x03\x04")
	write(t, dir, "t.txt", "b\n")
	var d gitx.Diff
	if err := call(t, service(dir), "git.diff", `{`+sid+`}`, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Binary) != 1 || d.Binary[0] != "img.bin" {
		t.Errorf("binary %v", d.Binary)
	}
	if !strings.Contains(d.Diff, "+b") {
		t.Errorf("text diff missing: %q", d.Diff)
	}
}

func TestLogLimitAndOrder(t *testing.T) {
	dir := newRepo(t)
	for _, n := range []string{"one", "two", "three"} {
		write(t, dir, n+".txt", n)
		git(t, dir, "add", "-A")
		git(t, dir, "commit", "-qm", n)
	}
	var lg struct{ Commits []gitx.Commit }
	if err := call(t, service(dir), "git.log", `{`+sid+`,"limit":2}`, &lg); err != nil {
		t.Fatal(err)
	}
	if len(lg.Commits) != 2 || lg.Commits[0].Subject != "three" || lg.Commits[1].Subject != "two" {
		t.Fatalf("commits %+v", lg.Commits)
	}
	c := lg.Commits[0]
	if len(c.Hash) != 40 || !strings.HasPrefix(c.Hash, c.Short) || c.Author != "t" || !strings.HasSuffix(c.Date, "Z") {
		t.Errorf("commit fields %+v", c)
	}
	if err := call(t, service(dir), "git.log", `{`+sid+`,"limit":0}`, nil); err == nil {
		t.Error("limit 0 accepted")
	}
}

func TestLogEmptyRepoIsEmpty(t *testing.T) {
	var lg struct{ Commits []gitx.Commit }
	if err := call(t, service(newRepo(t)), "git.log", `{`+sid+`}`, &lg); err != nil || len(lg.Commits) != 0 {
		t.Errorf("commits %+v err %v", lg.Commits, err)
	}
}

func TestBranchesUpstream(t *testing.T) {
	remote := newRepo(t)
	write(t, remote, "a.txt", "a")
	git(t, remote, "add", "-A")
	git(t, remote, "commit", "-qm", "init")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	git(t, dir, "clone", "-q", remote, "clone")
	clone := filepath.Join(dir, "clone")
	git(t, clone, "branch", "topic")

	var br gitx.Branches
	if err := call(t, service(clone), "git.branches", `{`+sid+`}`, &br); err != nil {
		t.Fatal(err)
	}
	if br.Current == nil || *br.Current != "main" || len(br.Branches) != 2 {
		t.Fatalf("branches %+v", br)
	}
	if b := br.Branches[0]; b.Name != "main" || !b.Current || b.Upstream == nil || *b.Upstream != "origin/main" {
		t.Errorf("main %+v", b)
	}
	if b := br.Branches[1]; b.Name != "topic" || b.Current || b.Upstream != nil {
		t.Errorf("topic %+v", b)
	}
}

func TestRefStartingWithDashRejected(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "a")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	s := service(dir)
	for _, m := range []string{"git.log", "git.diff"} {
		err := call(t, s, m, `{`+sid+`,"ref":"--output=`+filepath.Join(dir, "x")+`"}`, nil)
		if !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("%s: err %v", m, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Error("option was interpreted by git")
	}
	if err := call(t, s, "git.log", `{`+sid+`,"ref":"no-such-ref"}`, nil); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("unresolvable ref: %v", err)
	}
}

func TestDiffRejectsEscapingPath(t *testing.T) {
	dir := newRepo(t)
	for _, p := range []string{"../x", "/etc/passwd", "a/../../x"} {
		if err := call(t, service(dir), "git.diff", `{`+sid+`,"path":`+quote(p)+`}`, nil); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("path %q: %v", p, err)
		}
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestGitIgnoresRepoExecConfig(t *testing.T) {
	dir := newRepo(t)
	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "run.sh")
	write(t, filepath.Dir(script), "run.sh", "#!/bin/sh\ntouch '"+marker+"'\ncat \"$2\" 2>/dev/null\nexit 0\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	write(t, hooks, "reference-transaction", "#!/bin/sh\ntouch '"+marker+"'\n")
	write(t, dir, "a.dat", "one\n")
	write(t, dir, ".gitattributes", "*.dat diff=x\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	write(t, dir, "a.dat", "two\n")
	for _, kv := range [][2]string{
		{"core.fsmonitor", script},
		{"core.hooksPath", hooks},
		{"diff.external", script},
		{"diff.x.textconv", script},
		{"core.pager", "touch '" + marker + "'; cat"},
	} {
		git(t, dir, "config", kv[0], kv[1])
	}

	// The control proves the config is live: plain git runs the program.
	if err := exec.Command("git", "-C", dir, "--no-pager", "diff").Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Skipf("this git does not run the configured program: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	s := service(dir)
	for _, c := range [][2]string{
		{"git.status", `{` + sid + `}`},
		{"git.diff", `{` + sid + `}`},
		{"git.diff", `{` + sid + `,"staged":true,"ref":"HEAD"}`},
		{"git.log", `{` + sid + `}`},
		{"git.branches", `{` + sid + `}`},
	} {
		if err := call(t, s, c[0], c[1], nil); err != nil {
			t.Fatalf("%s: %v", c[0], err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s ran a program named by the repository config", c[0])
		}
	}
}

func TestEnvDropsGitVariables(t *testing.T) {
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_EXTERNAL_DIFF", "/bin/true")
	for _, kv := range gitx.Env() {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_EXTERNAL_DIFF=") {
			t.Errorf("%s leaked", kv)
		}
	}
}

func TestLogIgnoresSignatureProgram(t *testing.T) {
	dir := newRepo(t)
	marker := filepath.Join(t.TempDir(), "marker")
	prog := filepath.Join(t.TempDir(), "gpg.sh")
	write(t, filepath.Dir(prog), "gpg.sh", "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")
	if err := os.Chmod(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "a")
	git(t, dir, "add", "-A")
	tree := strings.TrimSpace(git(t, dir, "write-tree"))
	raw := "tree " + tree + "\nauthor t <t@example.com> 1700000000 +0000\ncommitter t <t@example.com> 1700000000 +0000\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n \n abc\n -----END PGP SIGNATURE-----\n\nsigned\n"
	file := filepath.Join(t.TempDir(), "commit")
	write(t, filepath.Dir(file), "commit", raw)
	hash := strings.TrimSpace(git(t, dir, "hash-object", "-t", "commit", "-w", file))
	git(t, dir, "update-ref", "HEAD", hash)
	git(t, dir, "config", "log.showSignature", "true")
	git(t, dir, "config", "gpg.program", prog)

	if err := exec.Command("git", "-C", dir, "--no-pager", "log").Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Skipf("this git does not run the signature program: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	var lg struct{ Commits []gitx.Commit }
	if err := call(t, service(dir), "git.log", `{`+sid+`}`, &lg); err != nil || len(lg.Commits) != 1 {
		t.Fatalf("log %+v err %v", lg.Commits, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("git.log ran the configured signature program")
	}
}

func fakeGit(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	write(t, d, "git", "#!/bin/sh\n"+body+"\n")
	p := filepath.Join(d, "git")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSlowGitTimesOutInsteadOfLookingLikeNoRepo(t *testing.T) {
	g := &gitx.Git{Bin: fakeGit(t, "exec sleep 5"), Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, err := g.Status(context.Background(), t.TempDir())
	if !errors.Is(err, gitx.ErrTimeout) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func TestToplevelOnlyExit128IsNotARepo(t *testing.T) {
	ctx := context.Background()
	if root, err := (&gitx.Git{Bin: fakeGit(t, "exit 128")}).Toplevel(ctx, "/"); err != nil || root != "" {
		t.Errorf("exit 128: %q %v", root, err)
	}
	var ee *gitx.ExitError
	if _, err := (&gitx.Git{Bin: fakeGit(t, "exit 1")}).Toplevel(ctx, "/"); !errors.As(err, &ee) {
		t.Errorf("exit 1: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (&gitx.Git{}).Toplevel(cancelled, "/"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
