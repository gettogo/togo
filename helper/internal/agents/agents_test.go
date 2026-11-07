package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
		if got := Installed(a, path, nil); got != tc.want {
			t.Errorf("Installed(%s) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestRegistryOrderAndLookup(t *testing.T) {
	var ids []string
	for _, a := range Registry {
		ids = append(ids, a.ID)
	}
	want := []string{
		"claude", "omp", "codex", "opencode", "gemini", "copilot", "cursor", "aider",
		"amp", "auggie", "cline", "crush", "droid", "goose", "kilo", "kimi", "kiro", "openhands",
		"pi", "vibe", "shell",
	}
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

func TestResume(t *testing.T) {
	if a, _ := Lookup("claude"); len(a.Resume) != 1 || a.Resume[0] != "--continue" {
		t.Errorf("claude Resume = %q, want [--continue]", a.Resume)
	}
	if a, _ := Lookup("codex"); len(a.Resume) != 2 || a.Resume[0] != "resume" || a.Resume[1] != "--last" {
		t.Errorf("codex Resume = %q, want [resume --last]", a.Resume)
	}
	if a, _ := Lookup("gemini"); len(a.Resume) != 2 || a.Resume[0] != "--resume" || a.Resume[1] != "latest" {
		t.Errorf("gemini Resume = %q, want [--resume latest]", a.Resume)
	}
	for _, id := range []string{"omp", "opencode", "amp", "cline", "goose", "kilo", "kiro", "openhands", "shell"} {
		if a, _ := Lookup(id); a.Resume != nil {
			t.Errorf("%s Resume = %q, want nil", id, a.Resume)
		}
	}
}

// Every entry has a unique wire id, and every command is a bare file name
// that Find can look up.
func TestRegistryEntriesAreWellFormed(t *testing.T) {
	idRe := regexp.MustCompile(`^[a-z0-9-]+$`)
	seen := map[string]bool{}
	for _, a := range Registry {
		if !idRe.MatchString(a.ID) || seen[a.ID] {
			t.Errorf("id %q is malformed or repeated", a.ID)
		}
		seen[a.ID] = true
		if a.Name == "" {
			t.Errorf("%s has no name", a.ID)
		}
		if a.Command != "" && (a.Command != filepath.Base(a.Command) || strings.ContainsAny(a.Command, " /")) {
			t.Errorf("%s command %q is not a bare name", a.ID, a.Command)
		}
		if a.Command == "" && a.ID != "shell" {
			t.Errorf("%s has no command", a.ID)
		}
	}
}

func TestFindUsesFallbackDirs(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "claude")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, off, ok := Find("claude", "/usr/bin:/bin", FallbackDirs(home))
	if !ok || !off || got != want {
		t.Fatalf("Find = %q, off %v, ok %v; want %q, off, ok", got, off, ok, want)
	}
	if _, off, ok := Find("claude", local, FallbackDirs(home)); !ok || off {
		t.Fatalf("Find on PATH = off %v, ok %v; want on PATH", off, ok)
	}
	if _, _, ok := Find("nosuchagent", "/usr/bin:/bin", FallbackDirs(home)); ok {
		t.Fatal("Find found a program that is not installed")
	}
	for _, d := range []string{"~/bin", ".bun/bin", ".npm-global/bin", ".cargo/bin", "go/bin", ".kimi-code/bin", "/opt/homebrew/bin", "/usr/local/bin"} {
		d = strings.Replace(d, "~/", "", 1)
		if !slices.Contains(FallbackDirs(home), d) && !slices.Contains(FallbackDirs(home), filepath.Join(home, d)) {
			t.Errorf("FallbackDirs lacks %s", d)
		}
	}
}

func TestResolveCustomProgram(t *testing.T) {
	for _, c := range []struct {
		id   string
		argv []string
		want string
	}{
		{"custom", []string{"/Users/u/.local/bin/kimi", "--yolo"}, "kimi"},
		{"custom", []string{"cursor-agent"}, "cursor"},
		{"custom", []string{"/usr/bin/htop"}, "custom"},
		{"custom", nil, "custom"},
		{"claude", []string{"/x/kimi"}, "claude"},
	} {
		if got := Resolve(c.id, c.argv); got != c.want {
			t.Errorf("Resolve(%q, %v) = %q, want %q", c.id, c.argv, got, c.want)
		}
	}
}
