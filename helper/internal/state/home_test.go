package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if got != "/proc/bin" || ok {
		t.Fatalf("loginPath with a missing shell = %q, %v; want the process PATH %q, not from the shell", got, ok, "/proc/bin")
	}
}

func TestLoginPathNotCachedAfterCancel(t *testing.T) {
	resetLoginPath(t)
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

func writeShell(t *testing.T, script string) string {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "fakesh")
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return shell
}

func resetLoginPath(t *testing.T) {
	reset := func() {
		login.mu.Lock()
		login.env, login.at, login.fresh = nil, time.Time{}, false
		login.cache, login.loaded = "", false
		login.failedAt, login.failures = time.Time{}, 0
		login.mu.Unlock()
	}
	reset()
	old := retryLater
	retryLater = func(time.Duration, func()) {}
	t.Cleanup(func() {
		waitLoginRead(t)
		reset()
		retryLater = old
	})
}

// waitLoginRead waits for a background read to end.
func waitLoginRead(t *testing.T) {
	t.Helper()
	login.mu.Lock()
	ch := login.running
	login.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal("the login shell read never ended")
		}
	}
}

// The agent launch command runs under "$SHELL -lic", and PATH additions
// often live in files only interactive shells read (~/.zshrc), so the
// lookup must ask an interactive shell too. The shell reads stdin first:
// anything but /dev/null could block it until the timeout.
func TestLoginPathUsesInteractiveShell(t *testing.T) {
	shell := writeShell(t, `#!/bin/sh
cat >/dev/null
echo 'rc noise'
case "$1" in *i*) PATH=/interactive/bin:/usr/bin ;; esac
eval "$2"
`)
	got, ok := loginPath(context.Background(), shell)
	if got != "/interactive/bin:/usr/bin" || !ok {
		t.Fatalf("loginPath = %q, %v; want the interactive PATH %q, cacheable", got, ok, "/interactive/bin:/usr/bin")
	}
}

// A shell that hangs (a prompt in .zshrc waiting for input, a slow network
// mount) must not leave the process PATH cached for the daemon's lifetime:
// the timeout result is used for now and the shell is asked again later.
func TestLoginPathDoesNotCacheTimeout(t *testing.T) {
	resetLoginPath(t)
	old := loginPathTimeout
	loginPathTimeout = 200 * time.Millisecond
	t.Cleanup(func() { loginPathTimeout = old })
	now := time.Now()
	loginPathNow = func() time.Time { return now }
	t.Cleanup(func() { loginPathNow = time.Now })
	t.Setenv("PATH", "/proc/bin")
	// sleep is a child of the shell and keeps stdout open after the shell
	// is killed, which must not stretch the wait.
	t.Setenv("SHELL", writeShell(t, "#!/bin/sh\n/bin/sleep 30\n"))

	start := time.Now()
	if got := LoginPath(context.Background()); got != "/proc/bin" {
		t.Fatalf("LoginPath with a hung shell = %q, want the process PATH", got)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("LoginPath with a hung shell took %v, want about the timeout", waited)
	}
	t.Setenv("SHELL", fakeShell(t))
	// Inside the retry window the shell is not asked again.
	if got := LoginPath(context.Background()); got != "/proc/bin" {
		t.Fatalf("LoginPath inside the retry window = %q, want the process PATH", got)
	}
	now = now.Add(loginPathRetry + time.Second)
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath after the retry window = %q, want the login PATH", got)
	}
	t.Setenv("SHELL", "/nonexistent/shell")
	if got := LoginPath(context.Background()); got != "/login/bin:/usr/bin" {
		t.Fatalf("LoginPath after a good read = %q, want it cached", got)
	}
}

func TestLoginPathReportsSource(t *testing.T) {
	resetLoginPath(t)
	t.Setenv("PATH", "/proc/bin")
	t.Setenv("SHELL", fakeShell(t))
	if _, fromShell := LoginPathSource(context.Background()); !fromShell {
		t.Fatal("source = fallback, want the login shell")
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
