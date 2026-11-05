package fsread_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/fsx"
)

// monorepo returns a repository whose session folder is apps/web, with
// README.md and apps/web/app.js committed and apps/tools/new.txt untracked.
func monorepo(t *testing.T) (root, cwd string) {
	t.Helper()
	root = tempDir(t)
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "README.md", []byte("top\n"))
	write(t, root, "apps/web/app.js", []byte("web\n"))
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "init")
	write(t, root, "apps/tools/new.txt", []byte("untracked\n"))
	return root, filepath.Join(root, "apps", "web")
}

func readRoot(t *testing.T, cwd, path, ref string) (string, error) {
	t.Helper()
	var r struct{ Content string }
	err := call(t, cwd, "fs.read", `{"id":"s_abcdefghij","root":true,"path":`+quote(path)+`,"ref":`+quote(ref)+`}`, &r)
	return r.Content, err
}

func TestReadRootRelativeFromSubfolder(t *testing.T) {
	_, cwd := monorepo(t)
	for _, tc := range []struct{ path, ref, want string }{
		{"apps/tools/new.txt", "", "untracked\n"},
		{"apps/web/app.js", "", "web\n"},
		{"README.md", "HEAD", "top\n"},
		{"apps/web/app.js", "HEAD", "web\n"},
	} {
		got, err := readRoot(t, cwd, tc.path, tc.ref)
		if err != nil || got != tc.want {
			t.Errorf("read %q at %q: %q, %v; want %q", tc.path, tc.ref, got, err, tc.want)
		}
	}
	// Without root the path stays relative to the session folder.
	if r, err := read(t, cwd, "app.js", "HEAD"); err != nil || r.Content != "web\n" {
		t.Errorf("session-relative read at HEAD: %+v, %v", r, err)
	}
	if _, err := read(t, cwd, "apps/web/app.js", ""); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("session-relative read of a root path: %v, want invalid path", err)
	}
}

func TestReadRootRefusesEscapes(t *testing.T) {
	root, cwd := monorepo(t)
	write(t, filepath.Dir(root), "outside.txt", []byte("no\n"))
	for _, p := range []string{"../outside.txt", "/etc/hosts", "apps/../../outside.txt"} {
		if _, err := readRoot(t, cwd, p, ""); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("read %q: %v, want invalid path", p, err)
		}
	}
	notRepo := tempDir(t)
	write(t, notRepo, "a.txt", []byte("a\n"))
	if _, err := readRoot(t, notRepo, "a.txt", ""); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("root read outside a repository: %v, want invalid path", err)
	}
}

func TestListDirRootRelativeFromSubfolder(t *testing.T) {
	_, cwd := monorepo(t)
	var r struct{ Entries []fsx.Entry }
	if err := call(t, cwd, "fs.listDir", `{"id":"s_abcdefghij","root":true,"path":"apps/tools","includeFiles":true}`, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) != 1 || r.Entries[0].Name != "new.txt" {
		t.Errorf("working tree entries %+v", r.Entries)
	}
	if err := call(t, cwd, "fs.listDir", `{"id":"s_abcdefghij","root":true,"path":"apps","ref":"HEAD"}`, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) != 1 || r.Entries[0].Name != "web" {
		t.Errorf("entries at HEAD %+v", r.Entries)
	}
	if err := call(t, cwd, "fs.listDir", `{"root":true,"path":"apps"}`, nil); err == nil {
		t.Error("root listing without id was accepted")
	}
	if err := call(t, cwd, "fs.listDir", `{"id":"s_abcdefghij","root":true,"path":".."}`, nil); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("root listing of ..: %v, want invalid path", err)
	}
}
