package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// useTestHost points the CLI at a private state directory and tmux label.
// SHELL is pinned so sessions never read the owner's shell startup files.
//
// TMUX_TMPDIR moves the default tmux socket into an empty private directory,
// so the owner's own sessions never show up as unmanaged rows. It is set
// before tmuxtest.New so that cleanup still sees it when it kills the server.
func useTestHost(t *testing.T) {
	t.Helper()
	t.Setenv("TMUX", "")
	// Short path: unix socket paths are limited to about 100 bytes.
	tmpdir, err := os.MkdirTemp("/tmp", "togo-cli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpdir) })
	t.Setenv("TMUX_TMPDIR", tmpdir)
	c := tmuxtest.New(t)
	t.Setenv("TOGO_HOME", t.TempDir())
	t.Setenv("TOGO_TMUX_LABEL", c.Socket[1])
	t.Setenv("SHELL", "/bin/sh")
}

// realPath resolves symlinks, because tmux reports a session's path in
// resolved form (/private/var on macOS) and ls shows what tmux reports.
func realPath(t *testing.T, dir string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runCLI(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, env, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestLsEmpty(t *testing.T) {
	useTestHost(t)
	code, stdout, stderr := runCLI(t, nil, "ls")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if want := "No sessions. Start one with: togo new\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestNewDetachThenLs(t *testing.T) {
	useTestHost(t)
	dir := t.TempDir()
	code, stdout, stderr := runCLI(t, nil, "new", "shell", "--dir", dir, "--detach")
	if code != 0 {
		t.Fatalf("new: code=%d stderr=%q", code, stderr)
	}
	if want := "Started 01 Shell in " + dir + "\n"; stdout != want {
		t.Errorf("new stdout = %q, want %q", stdout, want)
	}

	code, stdout, stderr = runCLI(t, nil, "ls")
	if code != 0 {
		t.Fatalf("ls: code=%d stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("ls printed %d lines, want header and one row:\n%s", len(lines), stdout)
	}
	if got := strings.Fields(lines[0]); !slices.Equal(got, []string{"#", "AGENT", "NAME", "STATUS", "PATH"}) {
		t.Errorf("header fields = %q", got)
	}
	row := strings.Fields(lines[1])
	if len(row) < 3 || row[0] != "01" || row[1] != "Shell" || row[len(row)-1] != realPath(t, dir) {
		t.Errorf("row = %q, want number 01, agent Shell, path %s", lines[1], realPath(t, dir))
	}
}

func TestNewWithNameAndDirUnderHome(t *testing.T) {
	useTestHost(t)
	home := realPath(t, t.TempDir())
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, nil, "new", "--name", "build", "--dir", proj, "--detach")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if want := "Started 01 build in ~/proj\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	_, stdout, _ = runCLI(t, nil, "ls")
	if !strings.Contains(stdout, "~/proj") {
		t.Errorf("ls does not show the home directory as ~:\n%s", stdout)
	}
}

func TestNewDefaultsToWorkingDirectory(t *testing.T) {
	useTestHost(t)
	t.Chdir(t.TempDir())
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, nil, "new", "--detach")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.HasSuffix(stdout, " in "+want+"\n") {
		t.Errorf("stdout = %q, want it to end with the working directory %s", stdout, want)
	}
}

func TestNewUnknownAgentFailsWithOneLine(t *testing.T) {
	useTestHost(t)
	code, stdout, stderr := runCLI(t, nil, "new", "nope", "--detach")
	if code != 1 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "unknown agent") {
		t.Errorf("stderr = %q, want one line naming the unknown agent", stderr)
	}
}

func TestAttachArgs(t *testing.T) {
	useTestHost(t)
	dir := t.TempDir()
	if code, _, stderr := runCLI(t, nil, "new", "--dir", dir, "--detach"); code != 0 {
		t.Fatalf("new: code=%d stderr=%q", code, stderr)
	}
	svc, err := newService(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	argv, environ, err := attachArgs(context.Background(), svc, "1", map[string]string{"TMUX": "/tmp/x,1,0", "HOME": "/h"})
	if err != nil {
		t.Fatal(err)
	}
	n := len(argv)
	if n < 3 || argv[n-3] != "attach-session" || argv[n-2] != "-t" || !strings.HasPrefix(argv[n-1], "=s_") {
		t.Errorf("argv = %q, want it to end with attach-session -t =s_...", argv)
	}
	for _, kv := range environ {
		if strings.HasPrefix(kv, "TMUX=") {
			t.Errorf("child environment still has %s", kv)
		}
	}
	if !slices.Contains(environ, "HOME=/h") {
		t.Errorf("child environment lost other variables: %q", environ)
	}
}

func TestAttachUnknownSession(t *testing.T) {
	useTestHost(t)
	code, _, stderr := runCLI(t, nil, "attach", "7")
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Errorf("code=%d stderr=%q, want exit 1 and a not found line", code, stderr)
	}
}

func TestAttachWithoutTargetIsUsageError(t *testing.T) {
	code, _, stderr := runCLI(t, nil, "attach")
	if code != 2 || !strings.Contains(stderr, "Usage") {
		t.Errorf("code=%d stderr=%q, want exit 2 and usage", code, stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, nil, "frobnicate")
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, "Usage") || !strings.Contains(stderr, "frobnicate") {
		t.Errorf("stderr = %q, want usage naming the command", stderr)
	}
}

func TestBadFlagIsUsageError(t *testing.T) {
	code, _, stderr := runCLI(t, nil, "new", "--bogus")
	if code != 2 || !strings.Contains(stderr, "Usage") {
		t.Errorf("code=%d stderr=%q, want exit 2 and usage", code, stderr)
	}
}

func TestNoArgumentsPrintsUsage(t *testing.T) {
	code, _, stderr := runCLI(t, nil)
	if code != 2 || !strings.Contains(stderr, "Usage") {
		t.Errorf("code=%d stderr=%q, want exit 2 and usage", code, stderr)
	}
}

func TestVersionPrintsBuildAndPlatform(t *testing.T) {
	code, stdout, _ := runCLI(t, nil, "version")
	if code != 0 || !strings.HasPrefix(stdout, "togo ") || !strings.HasSuffix(stdout, ")\n") {
		t.Errorf("code=%d stdout=%q, want \"togo <version> (<os>/<arch>)\"", code, stdout)
	}
}

func TestMissingTmuxReportsOneLine(t *testing.T) {
	t.Setenv("TOGO_HOME", t.TempDir())
	t.Setenv("TOGO_TMUX_LABEL", "togo-test-notmux")
	missing := filepath.Join(t.TempDir(), "tmux")
	orig := newService
	newService = func(ctx context.Context) (*session.Service, error) {
		s, err := orig(ctx)
		if err == nil {
			s.Tmux.Bin, s.User.Bin = missing, missing
		}
		return s, err
	}
	t.Cleanup(func() { newService = orig })
	code, _, stderr := runCLI(t, nil, "ls")
	if code != 1 || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "tmux") {
		t.Errorf("code=%d stderr=%q, want exit 1 and one line about tmux", code, stderr)
	}
}

func TestLsMarksUnmanagedSessionsWithDash(t *testing.T) {
	useTestHost(t)
	// With TMUX_TMPDIR private, plain tmux is a separate "default" server
	// that stands in for the owner's own.
	start := exec.Command("tmux", "new-session", "-d", "-s", "mine", "-c", "/")
	start.Env = os.Environ()
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start default-socket session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-server").Run() })

	code, stdout, stderr := runCLI(t, nil, "ls")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header and the one unmanaged row:\n%s", stdout)
	}
	if row := strings.Fields(lines[1]); len(row) < 4 || row[0] != "-" || row[1] != "Shell" || row[2] != "mine" {
		t.Errorf("row = %q, want bib -, agent Shell, name mine", lines[1])
	}
}

func TestNewAgentSessionReportsExit(t *testing.T) {
	useTestHost(t)
	t.Setenv("HOME", t.TempDir())
	bin := realPath(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(bin, "report")
	togo := filepath.Join(bin, "togo")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + report + ".tmp && mv " + report + ".tmp " + report + "\n"
	if err := os.WriteFile(togo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	origBinary, origService := togoBinary, newService
	togoBinary = func() (string, error) { return togo, nil }
	newService = func(ctx context.Context) (*session.Service, error) {
		svc, err := origService(ctx)
		if svc != nil {
			svc.Path = bin
		}
		return svc, err
	}
	t.Cleanup(func() { togoBinary, newService = origBinary, origService })
	// The pane's login shell looks claude up on PATH; only the stand-in
	// may be found there.
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	// Debian's /etc/profile replaces PATH, so restore the fixture's path
	// from this test home's login profile as a real user would.
	profile := "export PATH='" + strings.ReplaceAll(bin+":/usr/bin:/bin", "'", "'\\''") + "'\n"
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".profile"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runCLI(t, nil, "new", "claude", "--dir", t.TempDir(), "--detach"); code != 0 {
		t.Fatalf("new claude: code=%d stderr=%q", code, stderr)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		b, err := os.ReadFile(report)
		if err == nil {
			if string(b) != "session-exit\n3\n" {
				t.Fatalf("helper ran with %q, want session-exit 3", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a desk-started session never reported its exit: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
