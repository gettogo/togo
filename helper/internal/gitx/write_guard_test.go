package gitx_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestWriteRejectsEscapes(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	outside := t.TempDir()
	write(t, outside, "secret", "keep\n")
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"git.stage", "git.unstage", "git.discard"} {
		for _, p := range []string{"../x", "/etc/passwd", "a/../../x", "link/secret"} {
			if _, err := writeCall(t, s, m, []string{p}, nil); !errors.Is(err, fsx.ErrInvalidPath) {
				t.Errorf("%s %q: %v", m, p, err)
			}
		}
	}
	// The symlink itself is an untracked file: discarding it removes the
	// link, never what it points at.
	if _, err := writeCall(t, s, "git.discard", []string{"link"}, seenAll(t, s, "link")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "link")) || read(t, filepath.Join(outside, "secret")) != "keep\n" {
		t.Error("symlink discard went wrong")
	}
}

func TestWriteParamsValidated(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	long := strings.Repeat("a", wire.MaxWritePathBytes+1)
	many := make([]string, wire.MaxGitPaths+1)
	for i := range many {
		many[i] = fmt.Sprintf("f%d", i)
	}
	for name, params := range map[string]string{
		"no paths":    `{` + sid + `,"paths":[]}`,
		"nul":         pathsParams([]string{"a\x00b"}, nil),
		"long":        pathsParams([]string{long}, nil),
		"many":        pathsParams(many, nil),
		"folder":      pathsParams([]string{"dir/"}, nil),
		"bad seen":    pathsParams([]string{"README.md"}, map[string]wire.GitSeen{"x/": {Status: ".M"}}),
		"not strings": `{` + sid + `,"paths":[1]}`,
	} {
		if err := call(t, s, "git.stage", params, nil); !isInvalidParams(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWriteOnUnmanagedSession(t *testing.T) {
	dir := seeded(t)
	write(t, dir, "README.md", "x\n")
	un := &gitx.Service{Sessions: writeSessions{cwd: dir, unmanaged: true}}
	byID := writer(dir)
	for _, m := range []string{"git.stage", "git.unstage", "git.discard"} {
		if _, err := writeCall(t, un, m, []string{"README.md"}, nil); !errors.Is(err, session.ErrNotManaged) {
			t.Errorf("%s: %v", m, err)
		}
		params := `{"id":"u_6d7920776f726b","paths":["README.md"]}`
		if err := call(t, byID, m, params, nil); !errors.Is(err, session.ErrNotManaged) {
			t.Errorf("%s by id: %v", m, err)
		}
	}
	if err := call(t, un, "git.commit", `{`+sid+`,"message":"m","amend":false}`, nil); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("commit: %v", err)
	}
	if read(t, filepath.Join(dir, "README.md")) != "x\n" {
		t.Error("file changed")
	}
	// Reads still work.
	if l := letters(t, un, "README.md"); l != ".M" {
		t.Errorf("status on unmanaged session: %q", l)
	}
}

func TestStatusTruncatesAt2000(t *testing.T) {
	dir := seeded(t)
	for i := 0; i <= wire.MaxStatusFiles; i++ {
		write(t, dir, fmt.Sprintf("many/f%04d.txt", i), "x\n")
	}
	st := statusOf(t, writer(dir))
	if !st.Truncated || len(st.Files) != wire.MaxStatusFiles {
		t.Fatalf("truncated %v, %d files", st.Truncated, len(st.Files))
	}
	for _, f := range st.Files {
		if f.Fingerprint != "blob:587be6b4c3f93f93c489c0111bba5596147a26cb" {
			t.Fatalf("%s fingerprint %q", f.Path, f.Fingerprint)
		}
	}
	if st := statusOf(t, writer(seeded(t))); st.Truncated {
		t.Error("clean repo truncated")
	}
}

func TestStatusFingerprintsSymlinkByStat(t *testing.T) {
	dir := seeded(t)
	if err := os.Symlink("README.md", filepath.Join(dir, "ln")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dir, "ln"))
	if err != nil {
		t.Fatal(err)
	}
	want := wire.StatFingerprint(info.Size(), info.ModTime().UnixNano())
	if f := entries(statusOf(t, writer(dir)))["ln"]; f.Fingerprint != want {
		t.Errorf("got %q, want %q", f.Fingerprint, want)
	}
}

// mixedChanges gives a repository a modified tracked file, a file only in
// the index and an untracked file, and returns their paths.
func mixedChanges(t *testing.T, dir string) []string {
	t.Helper()
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "added.txt", "a\n")
	git(t, dir, "add", "added.txt")
	write(t, dir, "untracked.txt", "u\n")
	return []string{"README.md", "added.txt", "untracked.txt"}
}

func TestDiscardLockedChangesNothing(t *testing.T) {
	dir := seeded(t)
	s := writer(dir)
	paths := mixedChanges(t, dir)
	seen := seenAll(t, s, paths...)
	write(t, dir, ".git/index.lock", "held")
	_, err := writeCall(t, s, "git.discard", paths, seen)
	if r := conflictReason(err); r != wire.ConflictLocked {
		t.Fatalf("reason %q err %v", r, err)
	}
	if err := os.Remove(filepath.Join(dir, ".git", "index.lock")); err != nil {
		t.Fatal(err)
	}
	for p, l := range map[string]string{"README.md": ".M", "added.txt": "A.", "untracked.txt": "??"} {
		if got := letters(t, s, p); got != l {
			t.Errorf("%s is %q, want %q", p, got, l)
		}
	}
	// With the lock gone the same request goes through in one index write.
	if _, err := writeCall(t, s, "git.discard", paths, seen); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, s); len(st.Files) != 0 {
		t.Errorf("left %+v", st.Files)
	}
	if exists(filepath.Join(dir, "added.txt")) || exists(filepath.Join(dir, "untracked.txt")) {
		t.Error("files not deleted")
	}
}

func TestDiscardPartialListsChanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	dir := seeded(t)
	s := writer(dir)
	write(t, dir, "README.md", "hello again\n")
	write(t, dir, "locked/u.txt", "u\n")
	ro := filepath.Join(dir, "locked")
	seen := seenAll(t, s, "README.md", "locked/u.txt")
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	err := call(t, s, "git.discard", pathsParams([]string{"README.md", "locked/u.txt"}, seen), nil)
	var ce *api.CodeError
	if !errors.As(err, &ce) || conflictReason(err) != wire.ConflictPartial {
		t.Fatalf("got %v", err)
	}
	data, _ := ce.Data.(map[string]any)
	if changed, _ := data["changed"].([]string); len(changed) != 1 || changed[0] != "README.md" {
		t.Errorf("changed %v", data["changed"])
	}
	if read(t, filepath.Join(dir, "README.md")) != "hello\n" || !exists(filepath.Join(ro, "u.txt")) {
		t.Error("unexpected disk state")
	}
}

func TestDiscardInUnbornRepo(t *testing.T) {
	isolate(t)
	dir := newRepo(t)
	s := writer(dir)
	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "a.txt")
	write(t, dir, "b.txt", "b\n")
	if _, err := writeCall(t, s, "git.discard", []string{"a.txt", "b.txt"}, seenAll(t, s, "a.txt", "b.txt")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "a.txt")) || exists(filepath.Join(dir, "b.txt")) || len(statusOf(t, s).Files) != 0 {
		t.Error("discard before the first commit left changes")
	}
}

// wrappedGit returns a git that first runs action (sh) when one of its
// arguments equals trigger, then runs the real git.
func wrappedGit(t *testing.T, trigger, action string) gitx.Git {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	write(t, bin, "git", "#!/bin/sh\nfor a in \"$@\"; do\n\tif [ \"$a\" = '"+trigger+"' ]; then\n\t\t"+action+"\n\t\tbreak\n\tfi\ndone\nexec '"+real+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return gitx.Git{Bin: filepath.Join(bin, "git")}
}

func TestDiscardLockTakenMidWrite(t *testing.T) {
	dir := seeded(t)
	paths := mixedChanges(t, dir)
	plain := writer(dir)
	seen := seenAll(t, plain, paths...)
	lock := filepath.Join(dir, ".git", "index.lock")
	s := &gitx.Service{Sessions: writeSessions{cwd: dir},
		Git: wrappedGit(t, "restore", "printf held > '"+lock+"'")}
	_, err := writeCall(t, s, "git.discard", paths, seen)
	if r := conflictReason(err); r != wire.ConflictLocked {
		t.Fatalf("reason %q err %v", r, err)
	}
	if read(t, lock) != "held" {
		t.Error("lock changed")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	for p, l := range map[string]string{"README.md": ".M", "added.txt": "A.", "untracked.txt": "??"} {
		if got := letters(t, plain, p); got != l {
			t.Errorf("%s is %q, want %q", p, got, l)
		}
	}
}
