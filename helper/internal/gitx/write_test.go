package gitx_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

type writeSessions struct {
	cwd       string
	unmanaged bool
}

func (w writeSessions) Resolve(context.Context, string) (session.Session, error) {
	return session.Session{ID: "s_abcdefghij", Cwd: w.cwd, Managed: !w.unmanaged}, nil
}

func writer(dir string) *gitx.Service {
	return &gitx.Service{Sessions: writeSessions{cwd: dir}}
}

// isolate points HOME and XDG_CONFIG_HOME at empty directories so the
// user's own git config never reaches the helper's git processes.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// seeded returns a repository on main with an identity in its config and
// one commit adding README.md ("hello\n").
func seeded(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := newRepo(t)
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "config", "user.email", "t@example.com")
	write(t, dir, "README.md", "hello\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func statusOf(t *testing.T, s *gitx.Service) gitx.Status {
	t.Helper()
	var st gitx.Status
	if err := call(t, s, "git.status", `{`+sid+`}`, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func entries(st gitx.Status) map[string]gitx.FileStatus {
	m := map[string]gitx.FileStatus{}
	for _, f := range st.Files {
		m[f.Path] = f
	}
	return m
}

func letters(t *testing.T, s *gitx.Service, path string) string {
	t.Helper()
	f, ok := entries(statusOf(t, s))[path]
	if !ok {
		return ""
	}
	return f.Index + f.Worktree
}

// seenAll records what git.status reports now for paths, as the app would.
func seenAll(t *testing.T, s *gitx.Service, paths ...string) map[string]wire.GitSeen {
	t.Helper()
	all := entries(statusOf(t, s))
	seen := map[string]wire.GitSeen{}
	for _, p := range paths {
		f, ok := all[p]
		if !ok {
			t.Fatalf("%q not in status", p)
		}
		seen[p] = wire.GitSeen{Status: f.Index + f.Worktree, Fingerprint: f.Fingerprint}
	}
	return seen
}

func pathsParams(paths []string, seen map[string]wire.GitSeen) string {
	p := map[string]any{"id": "s_abcdefghij", "paths": paths}
	if seen != nil {
		p["seen"] = seen
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func writeCall(t *testing.T, s *gitx.Service, method string, paths []string, seen map[string]wire.GitSeen) (gitx.Status, error) {
	t.Helper()
	var st gitx.Status
	err := call(t, s, method, pathsParams(paths, seen), &st)
	return st, err
}

// conflictReason returns the -32006 reason of err, or "" for any other error.
func conflictReason(err error) string {
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != rpc.CodeConflict {
		return ""
	}
	b, _ := json.Marshal(ce.Data)
	var d struct{ Reason string }
	_ = json.Unmarshal(b, &d)
	return d.Reason
}

func isInvalidParams(err error) bool {
	var ip *session.InvalidParamsError
	return errors.As(err, &ip)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestStageModifiedAndUntracked(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "new.txt", "n\n")
	st, err := writeCall(t, s, "git.stage", []string{"README.md", "new.txt"}, seenAll(t, s, "README.md", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := entries(st)
	if f := got["README.md"]; f.Index != "M" || f.Worktree != "." || f.Fingerprint != "blob:13ab7f7412573d479aa8b41ce1e29a9f9f2a62d5" {
		t.Errorf("README.md %+v", f)
	}
	if f := got["new.txt"]; f.Index != "A" || f.Worktree != "." {
		t.Errorf("new.txt %+v", f)
	}
}

func TestStageRecordsDeletion(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	seen := seenAll(t, s, "README.md")
	if seen["README.md"].Fingerprint != "" {
		t.Errorf("deleted file has fingerprint %q", seen["README.md"].Fingerprint)
	}
	if _, err := writeCall(t, s, "git.stage", []string{"README.md"}, seen); err != nil {
		t.Fatal(err)
	}
	if l := letters(t, s, "README.md"); l != "D." {
		t.Errorf("letters %q", l)
	}
}

func TestUnstageInUnbornRepo(t *testing.T) {
	isolate(t)
	dir := newRepo(t)
	s := writer(dir)
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	write(t, dir, "a.txt", "b\n")
	if _, err := writeCall(t, s, "git.unstage", []string{"a.txt"}, seenAll(t, s, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if l := letters(t, s, "a.txt"); l != "??" {
		t.Errorf("letters %q", l)
	}
	if read(t, filepath.Join(dir, "a.txt")) != "b\n" {
		t.Error("working tree changed")
	}
}

func TestUnstageKeepsWorktree(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	git(t, dir, "add", "README.md")
	if _, err := writeCall(t, s, "git.unstage", []string{"README.md"}, seenAll(t, s, "README.md")); err != nil {
		t.Fatal(err)
	}
	if l := letters(t, s, "README.md"); l != ".M" {
		t.Errorf("letters %q", l)
	}
}

func TestDiscardTrackedMatchesHEAD(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "staged\n")
	git(t, dir, "add", "README.md")
	write(t, dir, "README.md", "unstaged\n")
	st, err := writeCall(t, s, "git.discard", []string{"README.md"}, seenAll(t, s, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 0 {
		t.Errorf("files %+v", st.Files)
	}
	if read(t, filepath.Join(dir, "README.md")) != "hello\n" {
		t.Error("content not restored")
	}
}

func TestDiscardIndexAddedFileRemovesIt(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "added.txt", "a\n")
	git(t, dir, "add", "added.txt")
	write(t, dir, "added.txt", "a then b\n")
	if _, err := writeCall(t, s, "git.discard", []string{"added.txt"}, seenAll(t, s, "added.txt")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "added.txt")) {
		t.Error("file still on disk")
	}
	if l := letters(t, s, "added.txt"); l != "" {
		t.Errorf("still in status as %q", l)
	}
}

// seenRename records a staged rename as the app does: the new path as listed
// and the old path with the same letters and no fingerprint.
func seenRename(t *testing.T, s *gitx.Service, newPath, oldPath string) map[string]wire.GitSeen {
	t.Helper()
	seen := seenAll(t, s, newPath)
	seen[oldPath] = wire.GitSeen{Status: seen[newPath].Status}
	return seen
}

func TestDiscardRenameRestoresOldPath(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "mv", "README.md", "GUIDE.md")
	if l := letters(t, s, "GUIDE.md"); l != "R." {
		t.Fatalf("rename shows as %q", l)
	}
	st, err := writeCall(t, s, "git.discard", []string{"GUIDE.md"}, seenRename(t, s, "GUIDE.md", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 0 {
		t.Errorf("files %+v", st.Files)
	}
	if exists(filepath.Join(dir, "GUIDE.md")) {
		t.Error("new path still on disk")
	}
	if read(t, filepath.Join(dir, "README.md")) != "hello\n" {
		t.Error("old path not restored")
	}
}

func TestDiscardRenameRefusesStale(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "mv", "README.md", "GUIDE.md")
	seen := seenRename(t, s, "GUIDE.md", "README.md")
	write(t, dir, "GUIDE.md", "edited after the phone looked\n")
	_, err := writeCall(t, s, "git.discard", []string{"GUIDE.md"}, seen)
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "GUIDE.md")) != "edited after the phone looked\n" || exists(filepath.Join(dir, "README.md")) {
		t.Error("the rename was touched")
	}
}

func TestDiscardRenameRefusesUnseenOldPath(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "mv", "README.md", "GUIDE.md")
	_, err := writeCall(t, s, "git.discard", []string{"GUIDE.md"}, seenAll(t, s, "GUIDE.md"))
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if !exists(filepath.Join(dir, "GUIDE.md")) || exists(filepath.Join(dir, "README.md")) {
		t.Error("the rename was touched")
	}
}

func TestDiscardRenameRefusesWhenOldPathIsBack(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "mv", "README.md", "GUIDE.md")
	write(t, dir, "README.md", "a new file under the old name\n")
	_, err := writeCall(t, s, "git.discard", []string{"GUIDE.md"}, seenRename(t, s, "GUIDE.md", "README.md"))
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "README.md")) != "a new file under the old name\n" {
		t.Error("the file at the old path was overwritten")
	}
}

func TestDiscardRenameRefusesIgnoredFileAtOldPath(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, ".gitignore", "README.md\n")
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-qm", "ignore")
	git(t, dir, "mv", "README.md", "GUIDE.md")
	seen := seenRename(t, s, "GUIDE.md", "README.md")
	write(t, dir, "README.md", "ignored, and the user's\n")
	_, err := writeCall(t, s, "git.discard", []string{"GUIDE.md"}, seen)
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "README.md")) != "ignored, and the user's\n" {
		t.Error("the ignored file was overwritten")
	}
	if read(t, filepath.Join(dir, "GUIDE.md")) != "hello\n" || letters(t, s, "GUIDE.md") != "R." {
		t.Error("the rename was touched")
	}
}

func TestDiscardCaseOnlyRenameRefused(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "Foo.txt", "foo\n")
	git(t, dir, "add", "Foo.txt")
	git(t, dir, "commit", "-qm", "foo")
	git(t, dir, "mv", "Foo.txt", "foo.txt")
	if l := letters(t, s, "foo.txt"); l != "R." {
		t.Fatalf("rename shows as %q", l)
	}
	_, err := writeCall(t, s, "git.discard", []string{"foo.txt"}, seenRename(t, s, "foo.txt", "Foo.txt"))
	if r := conflictReason(err); r != wire.ConflictCaseRename {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "foo.txt")) != "foo\n" || letters(t, s, "foo.txt") != "R." {
		t.Error("the rename was touched")
	}
}

func TestDiscardRenamePartialListsOldPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only folders")
	}
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "d/a.txt", "a\n")
	git(t, dir, "add", "d/a.txt")
	git(t, dir, "commit", "-qm", "a")
	git(t, dir, "mv", "d/a.txt", "b.txt")
	seen := seenRename(t, s, "b.txt", "d/a.txt")
	if err := os.Chmod(filepath.Join(dir, "d"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "d"), 0o755) })
	_, err := writeCall(t, s, "git.discard", []string{"b.txt"}, seen)
	if r := conflictReason(err); r != wire.ConflictPartial {
		t.Fatalf("reason %q err %v", r, err)
	}
	var ce *api.CodeError
	errors.As(err, &ce)
	b, _ := json.Marshal(ce.Data)
	var d struct{ Changed []string }
	_ = json.Unmarshal(b, &d)
	found := false
	for _, p := range d.Changed {
		found = found || p == "d/a.txt"
	}
	if !found {
		t.Errorf("changed %v leaves out d/a.txt", d.Changed)
	}
}

func TestDiscardUntrackedFolderKeepsIgnored(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, ".gitignore", "*.log\n")
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-qm", "ignore")
	write(t, dir, "newpkg/a.txt", "a\n")
	write(t, dir, "newpkg/b.txt", "b\n")
	write(t, dir, "newpkg/keep.log", "ignored\n")
	write(t, dir, "other/x.log", "ignored\n")

	st := statusOf(t, s)
	if _, ok := entries(st)["newpkg/a.txt"]; !ok {
		t.Fatalf("untracked files not listed one by one: %+v", st.Files)
	}
	for _, p := range []string{"newpkg/", "newpkg"} {
		if _, err := writeCall(t, s, "git.discard", []string{p}, nil); !isInvalidParams(err) {
			t.Errorf("%q: got %v, want invalid params", p, err)
		}
	}
	if _, err := writeCall(t, s, "git.discard", []string{"newpkg/a.txt"}, seenAll(t, s, "newpkg/a.txt")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "newpkg/a.txt")) {
		t.Error("a.txt still on disk")
	}
	for _, p := range []string{"newpkg/b.txt", "newpkg/keep.log", "other/x.log"} {
		if !exists(filepath.Join(dir, p)) {
			t.Errorf("%s was removed", p)
		}
	}
}

func TestDiscardRefusesUnmerged(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	git(t, dir, "checkout", "-qb", "side")
	write(t, dir, "README.md", "side\n")
	git(t, dir, "commit", "-qam", "side")
	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "README.md", "main\n")
	git(t, dir, "commit", "-qam", "main")
	cmd := exec.Command("git", "-C", dir, "merge", "-q", "side")
	cmd.Env = gitx.Env()
	_ = cmd.Run()
	before := read(t, filepath.Join(dir, "README.md"))
	_, err := writeCall(t, s, "git.discard", []string{"README.md"}, seenAll(t, s, "README.md"))
	if r := conflictReason(err); r != wire.ConflictUnmerged {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "README.md")) != before || letters(t, s, "README.md") != "UU" {
		t.Error("conflicted file changed")
	}
}

func TestDiscardRefusesStaleEntry(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "b.txt", "b\n")
	git(t, dir, "add", "b.txt")
	git(t, dir, "commit", "-qm", "b")
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "b.txt", "b2\n")
	seen := seenAll(t, s, "README.md", "b.txt")
	write(t, dir, "README.md", "hello\n")
	_, err := writeCall(t, s, "git.discard", []string{"b.txt", "README.md"}, seen)
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "b.txt")) != "b2\n" || letters(t, s, "b.txt") != ".M" {
		t.Error("another path was changed")
	}
}

func TestDiscardRefusesRepeatEdit(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello there\n")
	seen := seenAll(t, s, "README.md")
	write(t, dir, "README.md", "hello again\n")
	_, err := writeCall(t, s, "git.discard", []string{"README.md"}, seen)
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, filepath.Join(dir, "README.md")) != "hello again\n" {
		t.Error("file was discarded")
	}
	// The letters alone still match, and a seen without a fingerprint for a
	// file that has one is also stale.
	_, err = writeCall(t, s, "git.discard", []string{"README.md"}, map[string]wire.GitSeen{"README.md": {Status: ".M"}})
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("missing fingerprint: reason %q err %v", r, err)
	}
	_, err = writeCall(t, s, "git.discard", []string{"README.md"}, map[string]wire.GitSeen{"README.md": {Status: "M."}})
	if r := conflictReason(err); r != wire.ConflictStale {
		t.Fatalf("letters: reason %q err %v", r, err)
	}
}

func TestStageRefusesPathNotInStatus(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "other.txt", "o\n")
	for _, p := range []string{"README.md", "missing.txt"} {
		_, err := writeCall(t, s, "git.stage", []string{"other.txt", p}, nil)
		if r := conflictReason(err); r != wire.ConflictStale {
			t.Errorf("%s: reason %q err %v", p, r, err)
		}
	}
	if l := letters(t, s, "other.txt"); l != "??" {
		t.Errorf("other.txt staged anyway: %q", l)
	}
	// Unstage needs an index change, which a worktree-only edit is not.
	write(t, dir, "README.md", "x\n")
	if _, err := writeCall(t, s, "git.unstage", []string{"README.md"}, nil); conflictReason(err) != wire.ConflictStale {
		t.Errorf("unstage of unstaged change: %v", err)
	}
}

func TestWriteWhileIndexLocked(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	lock := filepath.Join(dir, ".git", "index.lock")
	write(t, dir, ".git/index.lock", "held by another git")
	for _, m := range []string{"git.stage", "git.discard"} {
		seen := map[string]wire.GitSeen{"README.md": {Status: ".M"}}
		if _, err := writeCall(t, s, m, []string{"README.md"}, seen); conflictReason(err) != wire.ConflictLocked {
			t.Errorf("%s: %v", m, err)
		}
	}
	if err := call(t, s, "git.commit", `{`+sid+`,"message":"m","amend":false}`, nil); conflictReason(err) != wire.ConflictLocked {
		t.Errorf("commit: %v", err)
	}
	if read(t, lock) != "held by another git" {
		t.Error("lock file changed")
	}
	if read(t, filepath.Join(dir, "README.md")) != "hello again\n" || letters(t, s, "README.md") != ".M" {
		t.Error("repository changed")
	}
}

// markerScript writes an executable that records it ran by creating marker.
func markerScript(t *testing.T) (script, marker string) {
	t.Helper()
	bin := t.TempDir()
	marker = filepath.Join(t.TempDir(), "marker")
	script = filepath.Join(bin, "run.sh")
	write(t, bin, "run.sh", "#!/bin/sh\necho \"$0 $*\" >> '"+marker+"'\ncat\nexit 0\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	return script, marker
}

func TestStageRefusesFilteredPath(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "t.bin", "tracked\n")
	write(t, dir, "s.bin", "same\n")
	git(t, dir, "add", "t.bin", "s.bin")
	git(t, dir, "commit", "-qm", "bin")
	script, marker := markerScript(t)
	write(t, dir, ".gitattributes", "*.bin filter=mark\n")
	git(t, dir, "config", "filter.mark.clean", script)
	git(t, dir, "config", "filter.mark.smudge", script)
	write(t, dir, "t.bin", "tracked, edited\n")
	write(t, dir, "u.bin", "untracked\n")
	for _, m := range []string{"git.stage", "git.discard"} {
		for _, p := range []string{"t.bin", "u.bin"} {
			_, err := writeCall(t, s, m, []string{p}, map[string]wire.GitSeen{p: {Status: letters(t, s, p)}})
			if r := conflictReason(err); r != wire.ConflictFiltered {
				t.Errorf("%s %s: reason %q err %v", m, p, r, err)
			}
		}
	}
	write(t, dir, "README.md", "changed\n")
	if _, err := writeCall(t, s, "git.stage", []string{"README.md", "u.bin"}, nil); conflictReason(err) != wire.ConflictFiltered {
		t.Errorf("stage: %v", err)
	}
	// Same bytes, new mtime: status must re-read s.bin, which runs a clean
	// filter unless the helper blanks it.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "s.bin"), later, later); err != nil {
		t.Fatal(err)
	}
	statusOf(t, s)
	if exists(marker) {
		t.Fatalf("filter program ran: %s", read(t, marker))
	}
	if letters(t, s, "README.md") != ".M" || letters(t, s, "u.bin") != "??" {
		t.Error("something was staged")
	}
	// The control proves the filter is live: plain git status runs it.
	cmd := exec.Command("git", "-C", dir, "status")
	cmd.Env = gitx.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !exists(marker) {
		t.Error("control: plain git status did not run the filter")
	}
}

func TestWriteLiteralOddNames(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	odd := []string{"-rf", "a b", "x*", "naïve.txt", "line\nbreak", "\"quoted\"", "tab\there", "trail\r"}
	for _, n := range append(odd, "xyz") {
		write(t, dir, n, "one\n")
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "odd")
	for _, n := range append(odd, "xyz") {
		write(t, dir, n, "two\n")
	}
	if _, err := writeCall(t, s, "git.stage", odd, seenAll(t, s, odd...)); err != nil {
		t.Fatal(err)
	}
	for _, n := range odd {
		if l := letters(t, s, n); l != "M." {
			t.Errorf("%q staged as %q", n, l)
		}
	}
	if l := letters(t, s, "xyz"); l != ".M" {
		t.Errorf("glob expanded: xyz is %q", l)
	}
	if _, err := writeCall(t, s, "git.discard", odd, seenAll(t, s, odd...)); err != nil {
		t.Fatal(err)
	}
	for _, n := range odd {
		if read(t, filepath.Join(dir, n)) != "one\n" {
			t.Errorf("%q not restored", n)
		}
	}
	if read(t, filepath.Join(dir, "xyz")) != "two\n" {
		t.Error("xyz discarded")
	}
}
