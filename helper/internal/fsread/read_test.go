package fsread_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/fsread"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/session"
)

type fakeSessions struct{ cwd string }

func (f fakeSessions) Resolve(context.Context, string) (session.Session, error) {
	return session.Session{ID: "s_abcdefghij", Cwd: f.cwd}, nil
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(gitx.Env(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func call(t *testing.T, cwd, method, params string, out any) error {
	t.Helper()
	s := &fsread.Service{Sessions: fakeSessions{cwd: cwd}}
	res, err := s.Methods()[method](context.Background(), json.RawMessage(params))
	if err != nil {
		return err
	}
	b, _ := json.Marshal(res)
	if out != nil {
		if uerr := json.Unmarshal(b, out); uerr != nil {
			t.Fatal(uerr)
		}
	}
	return nil
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func read(t *testing.T, cwd, path, ref string) (fsread.Result, error) {
	var r fsread.Result
	err := call(t, cwd, "fs.read", `{"id":"s_abcdefghij","path":`+quote(path)+`,"ref":`+quote(ref)+`}`, &r)
	return r, err
}

func TestReadWorkingTreeText(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "sub/a.txt", []byte("héllo\n"))
	r, err := read(t, dir, "sub/./a.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "sub/a.txt" || r.Encoding != "utf8" || r.Content != "héllo\n" || r.Size != 7 || r.Truncated {
		t.Errorf("result %+v", r)
	}
}

func TestReadRejectsTraversal(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "ok.txt", []byte("x"))
	for _, p := range []string{"../x", "/etc/passwd", "a/../../x", "", "missing.txt", "."} {
		if _, err := read(t, dir, p, ""); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("path %q: err %v", p, err)
		}
	}
}

func TestReadRejectsSymlinkEscape(t *testing.T) {
	outside := tempDir(t)
	write(t, outside, "secret.txt", []byte("secret"))
	dir := tempDir(t)
	write(t, dir, "inside.txt", []byte("fine"))
	for name, target := range map[string]string{
		"file-link": filepath.Join(outside, "secret.txt"),
		"dir-link":  outside,
		"up-link":   "../" + filepath.Base(outside) + "/secret.txt",
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("inside.txt", filepath.Join(dir, "ok-link")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"file-link", "dir-link/secret.txt", "up-link"} {
		if r, err := read(t, dir, p, ""); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("path %q: err %v, content %q", p, err, r.Content)
		}
	}
	if r, err := read(t, dir, "ok-link", ""); err != nil || r.Content != "fine" {
		t.Errorf("in-tree symlink: %+v %v", r, err)
	}
}

func TestReadBinaryAsBase64(t *testing.T) {
	dir := tempDir(t)
	raw := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 0xff, 0xfe}
	write(t, dir, "img.png", raw)
	write(t, dir, "latin1.txt", []byte("caf\xe9"))
	for _, name := range []string{"img.png", "latin1.txt"} {
		r, err := read(t, dir, name, "")
		if err != nil || r.Encoding != "base64" {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
		if strings.ContainsAny(r.Content, "=+/") {
			t.Errorf("%s: content is not base64url without padding: %q", name, r.Content)
		}
	}
	r, _ := read(t, dir, "img.png", "")
	if got, err := base64.RawURLEncoding.DecodeString(r.Content); err != nil || string(got) != string(raw) {
		t.Errorf("decoded %v %v", got, err)
	}
}

func TestReadTruncatesAtCap(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "big.txt", []byte(strings.Repeat("é", gitx.MaxOutput)))
	r, err := read(t, dir, "big.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || r.Size != 2*gitx.MaxOutput || r.Encoding != "utf8" || len(r.Content) > gitx.MaxOutput {
		t.Errorf("truncated %v size %d enc %s len %d", r.Truncated, r.Size, r.Encoding, len(r.Content))
	}
}

func commitFile(t *testing.T, dir, name string, content []byte) {
	write(t, dir, name, content)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "c")
}

func TestReadAtRef(t *testing.T) {
	dir := tempDir(t)
	git(t, dir, "init", "-q", "-b", "main")
	commitFile(t, dir, "pkg/f.txt", []byte("old\n"))
	commitFile(t, dir, "pkg/f.txt", []byte("new\n"))
	sub := filepath.Join(dir, "pkg")

	if r, err := read(t, sub, "f.txt", "HEAD~1"); err != nil || r.Content != "old\n" || r.Encoding != "utf8" {
		t.Errorf("HEAD~1 from subdir: %+v %v", r, err)
	}
	if r, err := read(t, sub, "f.txt", "HEAD"); err != nil || r.Content != "new\n" {
		t.Errorf("HEAD: %+v %v", r, err)
	}
	for _, c := range [][2]string{{"f.txt", "-n"}, {"f.txt", "--output=/tmp/x"}, {"missing.txt", "HEAD"}, {"f.txt", "nope"}, {".", "HEAD"}} {
		if _, err := read(t, sub, c[0], c[1]); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("path %q ref %q: err %v", c[0], c[1], err)
		}
	}
}

func TestListDirAtRef(t *testing.T) {
	dir := tempDir(t)
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "Beta/x", []byte("1"))
	write(t, dir, "alpha/x", []byte("1"))
	write(t, dir, ".hidden/x", []byte("1"))
	commitFile(t, dir, "top.txt", []byte("1"))

	type res struct {
		Path    string
		Entries []fsx.Entry
	}
	var r res
	if err := call(t, dir, "fs.listDir", `{"id":"s_abcdefghij","path":".","ref":"HEAD"}`, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) != 2 || r.Entries[0].Name != "alpha" || r.Entries[1].Name != "Beta" || r.Path != dir {
		t.Errorf("listing %+v", r)
	}
	if err := call(t, dir, "fs.listDir", `{"id":"s_abcdefghij","path":".","ref":"HEAD","showHidden":true}`, &r); err != nil || len(r.Entries) != 3 {
		t.Errorf("showHidden: %+v %v", r, err)
	}
	for _, params := range []string{
		`{"id":"s_abcdefghij","path":"..","ref":"HEAD"}`,
		`{"id":"s_abcdefghij","path":"top.txt","ref":"HEAD"}`,
		`{"id":"s_abcdefghij","path":".","ref":"-x"}`,
	} {
		if err := call(t, dir, "fs.listDir", params, nil); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("%s: %v", params, err)
		}
	}
}

func TestListDirWithoutRefListsAbsolutePath(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "d/x", []byte("1"))
	var r struct{ Entries []fsx.Entry }
	if err := call(t, dir, "fs.listDir", `{"path":`+quote(dir)+`}`, &r); err != nil || len(r.Entries) != 1 {
		t.Errorf("%+v %v", r, err)
	}
}

func TestListDirIncludeFiles(t *testing.T) {
	dir := tempDir(t)
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "src/x", []byte("1"))
	if err := os.Symlink("top.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "link")
	commitFile(t, dir, "top.txt", []byte("hello"))

	type res struct {
		Path    string
		Entries []fsx.Entry
	}
	five := int64(5)
	want := []fsx.Entry{
		{Name: "link", Kind: fsx.KindSymlink},
		{Name: "src", Dir: true, Kind: fsx.KindDir},
		{Name: "top.txt", Kind: fsx.KindFile, Size: &five},
	}
	var r res
	if err := call(t, dir, "fs.listDir", `{"id":"s_abcdefghij","path":".","ref":"HEAD","includeFiles":true}`, &r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Entries, want) {
		t.Errorf("at ref: %+v, want %+v", r.Entries, want)
	}
	r = res{}
	if err := call(t, dir, "fs.listDir", `{"path":`+quote(dir)+`,"includeFiles":true}`, &r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Entries, want) {
		t.Errorf("working tree: %+v, want %+v", r.Entries, want)
	}
	// Without includeFiles both forms keep listing folders only.
	r = res{}
	if err := call(t, dir, "fs.listDir", `{"id":"s_abcdefghij","path":".","ref":"HEAD"}`, &r); err != nil ||
		!reflect.DeepEqual(r.Entries, []fsx.Entry{{Name: "src", Dir: true}}) {
		t.Errorf("folders only: %+v %v", r.Entries, err)
	}
}
