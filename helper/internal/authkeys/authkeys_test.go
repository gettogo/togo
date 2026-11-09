package authkeys

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// phoneKey is the key a paired phone signs in with: an ed25519 blob of
// 0x11 bytes.
const phoneKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER"

// otherKey is a different ed25519 key, the user's own.
const otherKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIi"

// writeKeys writes content as ~/.ssh/authorized_keys under a temp home and
// returns its path.
func writeKeys(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func readKeys(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRemoveKeepsOtherBytes(t *testing.T) {
	in, err := os.ReadFile("testdata/authorized_keys")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/authorized_keys.want")
	if err != nil {
		t.Fatal(err)
	}
	path := writeKeys(t, string(in), 0o600)
	n, err := Remove(path, phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if got := readKeys(t, path); got != string(want) {
		t.Errorf("file after remove:\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveSkipsForeignComment(t *testing.T) {
	content := phoneKey + " me@laptop\n" + phoneKey + "\n" + phoneKey + " xtogo@phone\n"
	path := writeKeys(t, content, 0o600)
	n, err := Remove(path, phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("removed %d, want 0", n)
	}
	if got := readKeys(t, path); got != content {
		t.Errorf("file changed:\n%q", got)
	}
}

func TestRemoveHandlesOptionsAndCRLF(t *testing.T) {
	content := "# keys\r\n" +
		`restrict,command="x y, z" ` + phoneKey + " togo@Desk iPhone\r\n" +
		`from="10.0.0.1",command="echo \"ssh-ed25519 q\"" ` + otherKey + " togo@other\r\n" +
		otherKey + " me@laptop\r\n" +
		"no-pty\t" + phoneKey + "\ttogo@tab\r\n" +
		phoneKey + " togo@last"
	want := "# keys\r\n" +
		`from="10.0.0.1",command="echo \"ssh-ed25519 q\"" ` + otherKey + " togo@other\r\n" +
		otherKey + " me@laptop\r\n"
	path := writeKeys(t, content, 0o600)
	n, err := Remove(path, phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("removed %d, want 3", n)
	}
	if got := readKeys(t, path); got != want {
		t.Errorf("file after remove:\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveDuplicates(t *testing.T) {
	content := phoneKey + " togo@a\n" + otherKey + " me@laptop\n" + phoneKey + " togo@b\n"
	path := writeKeys(t, content, 0o600)
	n, err := Remove(path, phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if got, want := readKeys(t, path), otherKey+" me@laptop\n"; got != want {
		t.Errorf("file after remove %q, want %q", got, want)
	}
}

func TestRemoveRefusesSymlink(t *testing.T) {
	content := phoneKey + " togo@phone\n"
	target := writeKeys(t, content, 0o600)
	dir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "authorized_keys")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := Remove(link, phoneKey)
	var c *Conflict
	if !errors.As(err, &c) || c.Reason != ReasonSymlink {
		t.Fatalf("err = %v, want symlink conflict", err)
	}
	if got := readKeys(t, target); got != content {
		t.Errorf("target changed: %q", got)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link replaced: %v", err)
	}

	sub := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(sub, phoneKey); !errors.As(err, &c) || c.Reason != ReasonNotRegular {
		t.Errorf("directory: err = %v, want not_regular conflict", err)
	}
}

func TestRemoveMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	_, err := Remove(path, phoneKey)
	var c *Conflict
	if !errors.As(err, &c) || c.Reason != ReasonMissing {
		t.Fatalf("err = %v, want missing conflict", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file created: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("directory holds %d entries", len(entries))
	}
}

func TestRemoveKeepsMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644} {
		path := writeKeys(t, otherKey+" me@laptop\n"+phoneKey+" togo@phone\n", mode)
		if _, err := Remove(path, phoneKey); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode() != mode {
			t.Errorf("mode %v, want %v", fi.Mode(), mode)
		}
		entries, _ := os.ReadDir(filepath.Dir(path))
		if len(entries) != 2 {
			t.Errorf("want authorized_keys and its backup, got %d entries", len(entries))
		}
		bfi, err := os.Lstat(path + BackupSuffix)
		if err != nil {
			t.Fatal(err)
		}
		if bfi.Mode() != mode {
			t.Errorf("backup mode %v, want %v", bfi.Mode(), mode)
		}
	}
}

func TestRemoveWithoutMatchWritesNothing(t *testing.T) {
	path := writeKeys(t, otherKey+" me@laptop", 0o600)
	before, _ := os.Lstat(path)
	n, err := Remove(path, phoneKey)
	if err != nil || n != 0 {
		t.Fatalf("Remove = %d, %v", n, err)
	}
	after, _ := os.Lstat(path)
	if !os.SameFile(before, after) {
		t.Error("file rewritten without a match")
	}
}

func TestBadKeyRejected(t *testing.T) {
	path := writeKeys(t, phoneKey+" togo@phone\n", 0o600)
	for _, key := range []string{
		"",
		"ssh-ed25519",
		phoneKey + " togo@phone",
		"ssh-dss AAAAB3NzaC1kc3MAAACBAP",
		`restrict ` + phoneKey,
		"ssh-ed25519 AAAA\nssh-ed25519 AAAA",
		"ssh-ed25519 !!!!",
		// A valid blob under the wrong type name.
		"ssh-rsa AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER",
		// Not base64 of anything: the length is wrong.
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERE",
	} {
		if _, err := Remove(path, key); !errors.Is(err, ErrBadKey) {
			t.Errorf("%q: err = %v, want ErrBadKey", key, err)
		}
	}
	if got := readKeys(t, path); got != phoneKey+" togo@phone\n" {
		t.Errorf("file changed: %q", got)
	}
}

func TestDefaultPathWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "authorized_keys" || filepath.Base(filepath.Dir(path)) != ".ssh" {
		t.Errorf("DefaultPath = %q", path)
	}
}

// An exact key match removes only that line: the same key under a foreign
// comment, other Togo keys, comments and blank lines all stay.
func TestRemoveExactKeyOnly(t *testing.T) {
	// thirdKey is another phone's Togo key, sharing the phone key's type.
	const thirdKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMz"
	keep := "# my keys\n" +
		otherKey + " me@laptop\n" +
		"\n" +
		thirdKey + " togo-iPhone-9f8e7d6c\n" +
		phoneKey + " me@phone-import\n" +
		phoneKey[:len(phoneKey)-1] + "A togo-iPhone-1a2b3c4d\n"
	content := keep[:len("# my keys\n")] + phoneKey + " togo-iPhone-1a2b3c4d\n" + keep[len("# my keys\n"):]
	path := writeKeys(t, content, 0o600)
	n, err := Remove(path, phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("removed %d, want 1", n)
	}
	if got := readKeys(t, path); got != keep {
		t.Errorf("file after remove:\n%q\nwant\n%q", got, keep)
	}
	if got := readKeys(t, path+BackupSuffix); got != content {
		t.Errorf("backup %q, want the file as it was %q", got, content)
	}
}

// Current builds of the app name a device key "togo-<model>-<fingerprint>";
// earlier ones "togo@host". Both are Togo's.
func TestRemoveDeviceCommentFormats(t *testing.T) {
	for _, comment := range []string{"togo-iPhone-1a2b3c4d", "togo-iPad", "togo-device-ab12cd34", "togo@Sams-iPhone"} {
		path := writeKeys(t, otherKey+" me@laptop\n"+phoneKey+" "+comment+"\n", 0o600)
		n, err := Remove(path, phoneKey)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s: removed %d, want 1", comment, n)
		}
		if got, want := readKeys(t, path), otherKey+" me@laptop\n"; got != want {
			t.Errorf("%s: file after remove %q, want %q", comment, got, want)
		}
	}
}

func TestLines(t *testing.T) {
	path := writeKeys(t, "# c\n\n"+`restrict,command="a b" `+phoneKey+" togo-iPhone-1a2b3c4d\r\n"+otherKey+"\n", 0o600)
	got, err := Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key() != phoneKey || got[0].Comment != "togo-iPhone-1a2b3c4d" ||
		got[1].Key() != otherKey || got[1].Comment != "" {
		t.Errorf("Lines = %+v", got)
	}
}
