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
	r := &Recent{File: file}

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
	if len(got) != 20 || got[0] != "/d24" || got[19] != "/d5" {
		t.Fatalf("List after 25 adds = %v, want 20 entries from /d24 down to /d5", got)
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
	r := &Recent{File: filepath.Join(t.TempDir(), "nope.json")}
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
	r := &Recent{File: file}
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
	r := &Recent{File: file}
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
