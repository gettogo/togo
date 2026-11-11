package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecDirFallsBackToHomeNeverAParent(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ExecDir(deep, home); got != deep {
		t.Fatalf("existing folder: got %q, want it unchanged", got)
	}
	if got := ExecDir("", home); got != "" {
		t.Fatalf("empty: got %q, want empty", got)
	}
	if err := os.RemoveAll(filepath.Join(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	// The parent root/a still exists, but it is not the folder the user chose.
	if got := ExecDir(deep, home); got != home {
		t.Fatalf("deleted folder: got %q, want home %q", got, home)
	}
	if got := ExecDir(deep, filepath.Join(root, "no-home")); got != "" {
		t.Fatalf("deleted folder, no home: got %q, want empty", got)
	}
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ExecDir(file, home); got != home {
		t.Fatalf("a file: got %q, want home %q", got, home)
	}
}
