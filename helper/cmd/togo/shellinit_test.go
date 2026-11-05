package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func shellInitSnippet(t *testing.T, shell string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run([]string{"shell-init", shell}, nil, &out, &errOut); code != exitOK {
		t.Fatalf("shell-init %s: code %d: %s", shell, code, errOut.String())
	}
	return out.String()
}

// fakeTogo puts a togo on PATH that records its arguments one per line.
func fakeTogo(t *testing.T) (binDir, record string) {
	t.Helper()
	binDir = t.TempDir()
	record = filepath.Join(t.TempDir(), "argv")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + record + "'\n"
	if err := os.WriteFile(filepath.Join(binDir, "togo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir, record
}

func TestShellInitSnippetsQuoteDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `it's a "dir"; $(touch pwned)`)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		shell string
		argv  func(snippet string) []string
	}{
		{"bash", func(s string) []string { return []string{"-i", "-c", s} }},
		{"zsh", func(s string) []string { return []string{"-i", "-c", s} }},
		{"fish", func(s string) []string { return []string{"-i", "-c", s} }},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			bin, err := exec.LookPath(tc.shell)
			if err != nil {
				t.Skipf("%s not installed", tc.shell)
			}
			binDir, record := fakeTogo(t)
			snippet := shellInitSnippet(t, tc.shell)
			cmd := exec.Command(bin, tc.argv(snippet)...)
			cmd.Dir = dir
			cmd.Env = []string{"PATH=" + binDir + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "TOGO_SHELL_INIT=1"}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", tc.shell, err, out)
			}
			got, err := os.ReadFile(record)
			if err != nil {
				t.Fatalf("snippet did not run togo: %v", err)
			}
			want, _ := filepath.EvalSymlinks(dir)
			lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
			if len(lines) != 4 || strings.Join(lines[:3], " ") != "new shell --dir" {
				t.Fatalf("argv = %q, want new shell --dir <dir>", lines)
			}
			if gotDir, _ := filepath.EvalSymlinks(lines[3]); gotDir != want {
				t.Errorf("--dir = %q, want %q", lines[3], want)
			}
			if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
				t.Error("directory name was executed")
			}
		})
	}
}

func TestShellInitSnippetsStayOffUnlessEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
	}{
		{"variable unset", nil},
		{"variable not 1", []string{"TOGO_SHELL_INIT=0"}},
		{"inside tmux", []string{"TOGO_SHELL_INIT=1", "TMUX=/tmp/tmux-1/default,1,0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDir, record := fakeTogo(t)
			cmd := exec.Command("bash", "-i", "-c", shellInitSnippet(t, "bash"))
			cmd.Env = append([]string{"PATH=" + binDir + ":/usr/bin:/bin", "HOME=" + t.TempDir()}, tc.env...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bash: %v: %s", err, out)
			}
			if _, err := os.Stat(record); err == nil {
				t.Error("snippet started togo")
			}
		})
	}
}

func TestShellInitIgnoresNonInteractiveShells(t *testing.T) {
	binDir, record := fakeTogo(t)
	cmd := exec.Command("bash", "-c", shellInitSnippet(t, "bash"))
	cmd.Env = []string{"PATH=" + binDir + ":/usr/bin:/bin", "TOGO_SHELL_INIT=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash: %v: %s", err, out)
	}
	if _, err := os.Stat(record); err == nil {
		t.Error("a non-interactive shell started togo")
	}
}

func TestShellInitDefaultsToLoginShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"shell-init"}, map[string]string{"SHELL": "/usr/bin/fish"}, &out, &errOut); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "status is-interactive") {
		t.Errorf("snippet = %q, want the fish one", out.String())
	}
}

func TestShellInitRejectsUnknownShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"shell-init", "tcsh"}, nil, &out, &errOut); code != exitUsage || out.Len() != 0 {
		t.Errorf("code %d stdout %q, want a usage error with no snippet", code, out.String())
	}
}

func TestShellInitHelpExplains(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"shell-init", "--help"}, nil, &out, &errOut); code != exitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"TOGO_SHELL_INIT=1", "~/.zshrc", "does nothing"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}
