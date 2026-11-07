package state

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRecentOrderDedupeTrim(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recent.json")
	r := &Recent{File: file, Keeps: keepAll}

	for _, d := range []string{"/a", "/b", "/a"} {
		if err := r.Add(d); err != nil {
			t.Fatalf("Add(%q): %v", d, err)
		}
	}
	got, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"/a", "/b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}

	for i := range 25 {
		if err := r.Add(fmt.Sprintf("/d%d", i)); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	got, err = r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 || got[0] != "/d24" || got[4] != "/d20" {
		t.Fatalf("List after 25 adds = %v, want 5 entries from /d24 down to /d20", got)
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("recent file mode = %o, want 600", mode)
	}
}

func TestRecentListMissingFileIsEmpty(t *testing.T) {
	r := &Recent{File: filepath.Join(t.TempDir(), "nope.json"), Keeps: keepAll}
	got, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestRecentFileFormat(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recent.json")
	r := &Recent{File: file, Keeps: keepAll}
	if err := r.Add("/x"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if want := `{"dirs":["/x"]}` + "\n"; string(data) != want {
		t.Fatalf("file content = %q, want %q", data, want)
	}
}

func TestRecentCorruptFileIsEmptyAndReplaced(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recent.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Recent{File: file, Keeps: keepAll}
	got, err := r.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("List on a corrupt file = %v, %v; want empty, nil", got, err)
	}
	if err := r.Add("/x"); err != nil {
		t.Fatalf("Add on a corrupt file: %v", err)
	}
	if got, err := r.List(); err != nil || !reflect.DeepEqual(got, []string{"/x"}) {
		t.Fatalf("List after Add = %v, %v; want [/x]", got, err)
	}
}

func keepAll(string) bool { return true }

func TestRecentSkipsTempAndMissingFolders(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	gone := filepath.Join(home, "gone")
	scratch := filepath.Join(home, "scratchpad", "harness-e2e")
	for _, d := range []string{proj, gone, scratch} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := &Recent{File: filepath.Join(t.TempDir(), "recent.json")}
	for _, d := range []string{"/tmp", "/private/tmp/x", scratch, gone, proj} {
		if err := r.Add(d); err != nil {
			t.Fatalf("Add(%q): %v", d, err)
		}
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	got, _ := r.List()
	if want := []string{proj}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

func TestRecentForget(t *testing.T) {
	r := &Recent{File: filepath.Join(t.TempDir(), "recent.json"), Keeps: keepAll}
	for _, d := range []string{"/a", "/b", "/c"} {
		if err := r.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Forget("/b"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if err := r.Forget("/never"); err != nil {
		t.Fatalf("Forget of an unlisted folder: %v", err)
	}
	if got, _ := r.List(); !reflect.DeepEqual(got, []string{"/c", "/a"}) {
		t.Fatalf("List = %v, want [/c /a]", got)
	}
}

func TestIsTempDir(t *testing.T) {
	cases := []struct {
		dir, home, tmp string
		want           bool
	}{
		{"/tmp", "/Users/me", "", true},
		{"/tmp/x", "/Users/me", "", true},
		{"/private/tmp/workdir-01/s/scratchpad/harness-e2e/a", "/Users/me", "", true},
		{"/var/folders/ab/T/x", "/Users/me", "", true},
		{"/Users/me/t/x", "/Users/me", "/Users/me/t", false},
		{"/opt/tmpx/a", "/Users/me", "/opt/tmpx", true},
		{"/Users/me/scratchpad", "/Users/me", "", true},
		{"/Users/me/Dev/scratchpads", "/Users/me", "", false},
		{"/Users/me/Dev/app", "/Users/me", "/var/folders/ab/T", false},
		{"/tmpfiles/app", "/Users/me", "", false},
		// A home inside a temporary folder (tests) keeps its own folders.
		{"/private/var/folders/ab/T/home/proj", "/private/var/folders/ab/T/home", "/var/folders/ab/T", false},
	}
	for _, c := range cases {
		if got := IsTempDir(c.dir, c.home, c.tmp); got != c.want {
			t.Errorf("IsTempDir(%q, home %q, tmp %q) = %v, want %v", c.dir, c.home, c.tmp, got, c.want)
		}
	}
}
