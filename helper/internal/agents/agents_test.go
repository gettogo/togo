package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalled(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "omp"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "relative/dir" + string(filepath.ListSeparator) + bin

	cases := []struct {
		id   string
		want bool
	}{
		{"omp", true},
		{"codex", false},
		{"claude", false},
		{"opencode", false},
		{"shell", true},
	}
	for _, tc := range cases {
		a, ok := Lookup(tc.id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", tc.id)
		}
		if got := Installed(a, path); got != tc.want {
			t.Errorf("Installed(%s) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestRegistryOrderAndLookup(t *testing.T) {
	var ids []string
	for _, a := range Registry {
		ids = append(ids, a.ID)
	}
	want := []string{"claude", "omp", "codex", "opencode", "shell"}
	if len(ids) != len(want) {
		t.Fatalf("Registry ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("Registry ids = %v, want %v", ids, want)
		}
	}
	if a, _ := Lookup("claude"); a.Name != "Claude Code" || a.Command != "claude" {
		t.Errorf("Lookup(claude) = %+v", a)
	}
	if a, _ := Lookup("shell"); a.Command != "" {
		t.Errorf("shell Command = %q, want empty", a.Command)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) found an agent, want none")
	}
}
