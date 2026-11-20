package tape

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A tape is written only with the full commit it was made from, and a
// dirty tree needs TOGO_TAPE_ALLOW_DIRTY=1.
func TestRevisionGate(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a")
	gitIn(t, dir, "commit", "-q", "-m", "a")
	head := gitIn(t, dir, "rev-parse", "HEAD")

	t.Setenv("TOGO_TAPE_ALLOW_DIRTY", "")
	t.Setenv("TOGO_TAPE_REVISION", "")
	if _, err := SourceFromEnv(dir); err == nil {
		t.Error("no revision was accepted")
	}
	t.Setenv("TOGO_TAPE_REVISION", head[:12])
	if _, err := SourceFromEnv(dir); err == nil {
		t.Error("a short revision was accepted")
	}
	t.Setenv("TOGO_TAPE_REVISION", strings.Repeat("a", 40))
	if _, err := SourceFromEnv(dir); err == nil {
		t.Error("a revision that is not the tree's was accepted")
	}
	t.Setenv("TOGO_TAPE_REVISION", head)
	src, err := SourceFromEnv(dir)
	if err != nil || src.Revision != head {
		t.Fatalf("clean tree: %+v, %v", src, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceFromEnv(dir); err == nil {
		t.Error("a dirty tree was accepted")
	}
	t.Setenv("TOGO_TAPE_ALLOW_DIRTY", "1")
	if _, err := SourceFromEnv(dir); err != nil {
		t.Errorf("a dirty tree with TOGO_TAPE_ALLOW_DIRTY=1: %v", err)
	}
	tp := &Tape{Lines: [][]byte{[]byte(`{}`)}}
	if err := tp.Write(t.TempDir(), "x", Source{}); err == nil {
		t.Error("a tape was written without a revision")
	}
}

// The runner turns a failure into an error, a skip into Skip, and runs
// the cleanups.
func TestRunner(t *testing.T) {
	r := Runner{Scratch: t.TempDir()}
	ran := false
	err := r.Do("x", func(tb testing.TB) error {
		tb.Cleanup(func() { ran = true })
		tb.Fatalf("boom %d", 7)
		return nil
	})
	if err == nil || err.Error() != "boom 7" || !ran {
		t.Errorf("fatal: %v, cleanup ran %v", err, ran)
	}
	err = r.Do("x", func(tb testing.TB) error { tb.Skip("not here"); return nil })
	if s, ok := err.(Skip); !ok || s != "not here" {
		t.Errorf("skip: %#v", err)
	}
	if err := r.Do("x", func(tb testing.TB) error { _ = tb.TempDir(); return nil }); err != nil {
		t.Errorf("ok: %v", err)
	}
}
