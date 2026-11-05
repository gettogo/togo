package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func togoArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "togo", Size: int64(len(binary)), Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	tw.Write(binary)
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestReplaceRestoresPreviousWhenRenameFails(t *testing.T) {
	failRename := errors.New("rename refused")
	rename = func(string, string) error { return failRename }
	t.Cleanup(func() { rename = os.Rename })

	for _, hadPrevious := range []bool{false, true} {
		dir := t.TempDir()
		exe := filepath.Join(dir, "togo")
		if err := os.WriteFile(exe, []byte("current"), 0o755); err != nil {
			t.Fatal(err)
		}
		want := []string{"togo"}
		if hadPrevious {
			if err := os.WriteFile(exe+PreviousSuffix, []byte("older"), 0o755); err != nil {
				t.Fatal(err)
			}
			want = []string{"togo", "togo.previous"}
		}
		if err := replace(exe, togoArchive(t, []byte("new"))); !errors.Is(err, failRename) {
			t.Fatalf("replace = %v, want the rename failure", err)
		}
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if !slices.Equal(names, want) {
			t.Errorf("previous=%v: directory holds %q, want %q", hadPrevious, names, want)
		}
		if data, _ := os.ReadFile(exe); string(data) != "current" {
			t.Errorf("binary = %q, want it untouched", data)
		}
		if hadPrevious {
			if data, _ := os.ReadFile(exe + PreviousSuffix); string(data) != "older" {
				t.Errorf("togo.previous = %q, want the older binary restored", data)
			}
		}
	}
}

func TestReplaceSucceedsOverOlderPrevious(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "togo")
	os.WriteFile(exe, []byte("current"), 0o755)
	os.WriteFile(exe+PreviousSuffix, []byte("older"), 0o755)
	if err := replace(exe, togoArchive(t, []byte("new"))); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries, want togo and togo.previous", len(entries))
	}
	if data, _ := os.ReadFile(exe + PreviousSuffix); string(data) != "current" {
		t.Errorf("togo.previous = %q, want the replaced binary", data)
	}
}
