package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHomeCreatesPrivateDir(t *testing.T) {
	want := filepath.Join(t.TempDir(), "togo")
	t.Setenv("TOGO_HOME", want)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != want {
		t.Fatalf("Home = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("home mode = %v, want a directory with mode 700", info.Mode())
	}
}

func TestWriteFileAtomicPrivate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := WriteFile(file, []byte("one")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(file, []byte("two")); err != nil {
		t.Fatalf("WriteFile again: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "two" {
		t.Fatalf("ReadFile = %q, %v; want %q", data, err, "two")
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory holds %d entries (%v), want only the target file", len(entries), err)
	}
}

// fakeShell writes a login shell stand-in that prints profile noise around
// what it is asked to run, the way a motd or an echo in .zprofile does.
func fakeShell(t *testing.T) string {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "fakesh")
	script := "#!/bin/sh\necho 'Welcome to the machine'\nPATH=/login/bin:/usr/bin\neval \"$2\"\necho done\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return shell
}

func TestLoginPathIgnoresProfileNoise(t *testing.T) {
	got, ok := loginPath(context.Background(), fakeShell(t))
	if got != "/login/bin:/usr/bin" || !ok {
		t.Fatalf("loginPath = %q, %v; want %q, true", got, ok, "/login/bin:/usr/bin")
	}
}

func TestLoginPathFallsBackToProcessPath(t *testing.T) {
	t.Setenv("PATH", "/proc/bin")
	got, ok := loginPath(context.Background(), "/nonexistent/shell")
	if got != "/proc/bin" || !ok {
		t.Fatalf("loginPath with a missing shell = %q, %v; want the process PATH %q, cacheable", got, ok, "/proc/bin")
	}
}

func TestLoginPathNotCachedAfterCancel(t *testing.T) {
	loginPathMu.Lock()
	loginPathCached = ""
	loginPathMu.Unlock()
	t.Setenv("SHELL", fakeShell(t))
	t.Setenv("PATH", "/proc/bin")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := LoginPath(cancelled); got != "/proc/bin" {
		t.Fatalf("LoginPath(cancelled) = %q, want the process PATH", got)
	}
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath after a cancelled call = %q, want the login PATH", got)
	}
}

func TestHomeTightensExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "togo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOGO_HOME", dir)
	if _, err := Home(); err != nil {
		t.Fatalf("Home: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("existing home mode = %v, %v; want 700", info.Mode(), err)
	}
}
