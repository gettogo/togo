package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "proj"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "notes.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(filepath.Join(home, "proj"), link); err != nil {
		t.Fatal(err)
	}

	ok := []struct{ in, want string }{
		{"~", home},
		{"~/proj", filepath.Join(home, "proj")},
		{home + "/proj/../proj/", filepath.Join(home, "proj")},
		{link, link},
	}
	for _, tc := range ok {
		got, err := ValidateDir(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ValidateDir(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.want)
		}
	}

	bad := []string{"proj", "~proj", "", home + "/a\x00b", file, filepath.Join(home, "missing")}
	for _, in := range bad {
		if _, err := ValidateDir(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ValidateDir(%q) error = %v, want ErrInvalidPath", in, err)
		}
	}
}

func TestListDirHiddenAndSort(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"B", "a", ".h"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "c-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "f"), filepath.Join(dir, "f-link")); err != nil {
		t.Fatal(err)
	}

	path, entries, err := ListDir(dir+"/.", false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if path != dir {
		t.Errorf("path = %q, want cleaned %q", path, dir)
	}
	want := []Entry{{Name: "a", Dir: true}, {Name: "B", Dir: true}, {Name: "c-link", Dir: true}}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %v, want %v", entries, want)
	}

	_, entries, err = ListDir(dir, true)
	if err != nil {
		t.Fatalf("ListDir showHidden: %v", err)
	}
	want = []Entry{{Name: ".h", Dir: true}, {Name: "a", Dir: true}, {Name: "B", Dir: true}, {Name: "c-link", Dir: true}}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries with hidden = %v, want %v", entries, want)
	}
}

func TestListDirEmptyIsNonNil(t *testing.T) {
	_, entries, err := ListDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if entries == nil {
		t.Fatal("entries = nil, want an empty slice so it encodes as []")
	}
}

func TestListDirCapsEntries(t *testing.T) {
	dir := t.TempDir()
	for i := range maxEntries + 5 {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprintf("d%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, entries, err := ListDir(dir, false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != maxEntries {
		t.Fatalf("len(entries) = %d, want %d", len(entries), maxEntries)
	}
}

func TestListDirRejectsRelative(t *testing.T) {
	if _, _, err := ListDir("proj", false); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("ListDir(relative) error = %v, want ErrInvalidPath", err)
	}
}

// A folder the user cannot open (mode 0, or macOS privacy protection on
// ~/Library/Mail) is a path the client picked, not a helper failure.
func TestListDirUnreadableIsInvalidPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 0 directory")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, _, err := ListDir(dir, false); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("ListDir(unreadable) error = %v, want ErrInvalidPath", err)
	}
}

func TestListFilesKindsAndSizes(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	for _, d := range []string{"B", ".h"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Links are reported as links and never followed, wherever they point.
	if err := os.Symlink(outside, filepath.Join(dir, "out-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "out-file")); err != nil {
		t.Fatal(err)
	}

	path, entries, err := ListFiles(dir+"/.", false)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if path != dir {
		t.Errorf("path = %q, want %q", path, dir)
	}
	five := int64(5)
	want := []Entry{
		{Name: "a.txt", Kind: KindFile, Size: &five},
		{Name: "B", Dir: true, Kind: KindDir},
		{Name: "out-dir", Kind: KindSymlink},
		{Name: "out-file", Kind: KindSymlink},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %+v, want %+v", entries, want)
	}

	_, entries, err = ListFiles(dir, true)
	if err != nil || len(entries) != 5 || entries[0].Name != ".h" {
		t.Errorf("showHidden: %+v %v", entries, err)
	}
	for _, bad := range []string{"rel", dir + "/a.txt", dir + "/missing", dir + "/a\x00b"} {
		if _, _, err := ListFiles(bad, false); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ListFiles(%q) error = %v, want ErrInvalidPath", bad, err)
		}
	}
}

func TestListFilesCapsEntries(t *testing.T) {
	dir := t.TempDir()
	for i := range maxEntries + 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, entries, err := ListFiles(dir, false)
	if err != nil || len(entries) != maxEntries {
		t.Fatalf("len = %d, %v; want %d", len(entries), err, maxEntries)
	}
}
