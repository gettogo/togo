package tmux_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// TestVersionLive performs one real Version call against a private server.
// The pure parse cases live in version_test.go (package tmux) because
// parseVersion is unexported and package tmux cannot import tmuxtest.
func TestVersionLive(t *testing.T) {
	c := tmuxtest.New(t)
	major, minor, raw, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if major < 1 || minor < 0 || raw == "" {
		t.Fatalf("Version = %d.%d, raw %q, want a plausible parsed version", major, minor, raw)
	}
}

func TestListEmptyServer(t *testing.T) {
	c := tmuxtest.New(t)
	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions != nil {
		t.Fatalf("ListSessions = %v, want nil for a fresh label", sessions)
	}
}

func TestNewListKill(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	dir := t.TempDir()
	const name = "s_abcdefghij"
	const title = `it's "x"; $(rm -rf ~)`

	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    name,
		Dir:     dir,
		Cols:    100,
		Rows:    30,
		Options: map[string]string{"@togo_bib": "01", "@togo_name": title},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d sessions, want exactly one", len(sessions))
	}
	s := sessions[0]
	if s.Name != name {
		t.Errorf("Name = %q, want %q", s.Name, name)
	}
	if s.Bib != "01" {
		t.Errorf("Bib = %q, want %q", s.Bib, "01")
	}
	if s.Title != title {
		t.Errorf("Title = %q, want %q", s.Title, title)
	}
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	gotPath, err := filepath.EvalSymlinks(s.Path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", s.Path, err)
	}
	if gotPath != wantDir {
		t.Errorf("Path = %q, want %q", gotPath, wantDir)
	}

	if err := c.KillSession(ctx, name); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	sessions, err = c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions after kill: %v", err)
	}
	if sessions != nil {
		t.Fatalf("ListSessions after kill = %v, want nil", sessions)
	}
}

func TestKillMissing(t *testing.T) {
	c := tmuxtest.New(t)
	err := c.KillSession(context.Background(), "nope")
	if !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("KillSession error = %v, want errors.Is(err, ErrNoSession)", err)
	}
}

func TestListSessionsNewlineInOption(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const name = "s_nl_opt"

	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    name,
		Dir:     t.TempDir(),
		Options: map[string]string{"@togo_bib": "01", "@togo_name": "a\nb"},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var found bool
	for _, s := range sessions {
		if s.Name == name {
			found = true
			if s.Title != "a\nb" {
				t.Errorf("Title = %q, want %q", s.Title, "a\nb")
			}
			if s.Bib != "01" {
				t.Errorf("Bib = %q, want %q", s.Bib, "01")
			}
		}
	}
	if !found {
		t.Fatalf("ListSessions did not return session %q (got %d sessions)", name, len(sessions))
	}
}

// TestListSessionsNewlineInSessionName uses a session whose name contains a
// newline. The session is created through the Client's own argv path
// (NewSession builds "new-session -d -s NAME" with the name as one argv
// element, no shell). tmux 3.6a accepts such a name but sanitizes it: the
// server replaces the raw newline with the two-byte sequence backslash-n,
// so the stored (and listed) name is the 8-byte string "my\nwork". The
// test asserts that exact stored form and that ListSessions still returns
// the session. The raw-newline record-splitting path is exercised by the
// pure parseListOutput tests in parse_test.go.
func TestListSessionsNewlineInSessionName(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const rawName = "my\nwork"

	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: rawName, Dir: t.TempDir()}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d sessions, want exactly one", len(sessions))
	}
	const storedName = `my\nwork`
	if sessions[0].Name != storedName {
		t.Fatalf("Name = %q, want the tmux-stored form %q", sessions[0].Name, storedName)
	}
}

func TestSetOptionRejectsSeparators(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const name = "s_separators"

	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: name, Dir: t.TempDir()}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	cases := []struct {
		key   string
		value string
	}{
		{key: "@togo_name", value: "a\x1fb"},
		{key: "@togo_name", value: "a\x1eb"},
		{key: "@togo_name\x1f", value: "v"},
	}
	for _, tc := range cases {
		if err := c.SetOption(ctx, name, tc.key, tc.value); err == nil {
			t.Errorf("SetOption(%q, %q) = nil error, want an error", tc.key, tc.value)
		}
	}

	// None of the rejected calls may have reached tmux: @togo_name must
	// still be unset.
	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, s := range sessions {
		if s.Name == name && s.Title != "" {
			t.Errorf("@togo_name = %q after rejected SetOption calls, want empty", s.Title)
		}
	}
}

func TestSetOptionNoServer(t *testing.T) {
	c := tmuxtest.New(t)
	err := c.SetOption(context.Background(), "nope", "@togo_name", "v")
	if !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("SetOption on a fresh label: error = %v, want errors.Is(err, ErrNoSession)", err)
	}
}

func TestCommandRunsWithoutShell(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const title = `semi; $(echo injected)`
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name: "s_cmd",
		Dir:  dir,
		// The marker path is passed as a separate argv element, so sh writes
		// to it without any shell quoting or injection concern.
		Command: []string{"/bin/sh", "-c", "echo hi > \"$0\"; sleep 30", marker},
		Options: map[string]string{"@togo_name": title},
	}); err != nil {
		t.Fatalf("NewSession with command: %v", err)
	}

	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions returned %d sessions, want exactly one", len(sessions))
	}
	if sessions[0].Title != title {
		t.Errorf("Title = %q, want literal %q", sessions[0].Title, title)
	}

	// Prove the command actually ran: poll up to 2 seconds for the marker
	// file to appear with the expected content.
	deadline := time.Now().Add(2 * time.Second)
	var content []byte
	var readErr error
	for {
		content, readErr = os.ReadFile(marker)
		if readErr == nil && strings.Contains(string(content), "hi") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker file %q does not contain %q within 2s (content %q, err %v)",
				marker, "hi", content, readErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAttachArgv(t *testing.T) {
	c := tmux.NewLabel("togo")
	want := []string{"tmux", "-u", "-L", "togo", "attach-session", "-t", "=s_x"}
	if got := c.AttachArgv("s_x"); !reflect.DeepEqual(got, want) {
		t.Fatalf("AttachArgv = %v, want %v", got, want)
	}
}

func TestNotInstalled(t *testing.T) {
	c := &tmux.Client{Bin: "/nonexistent/tmux"}
	_, err := c.ListSessions(context.Background())
	if !errors.Is(err, tmux.ErrNotInstalled) {
		t.Fatalf("ListSessions error = %v, want errors.Is(err, ErrNotInstalled)", err)
	}
}

func TestNewSessionEnv(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	const name = "s_env"
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name: name,
		Dir:  t.TempDir(),
		Env:  map[string]string{"TOGO_SESSION": name, "TOGO_BIB": "07"},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for k, want := range map[string]string{"TOGO_SESSION": name, "TOGO_BIB": "07"} {
		args := append(append([]string{}, c.Socket...), "show-environment", "-t", "="+name, k)
		out, err := exec.Command(c.Bin, args...).Output()
		if err != nil {
			t.Fatalf("show-environment %s: %v", k, err)
		}
		if got := strings.TrimSpace(string(out)); got != k+"="+want {
			t.Errorf("show-environment %s = %q, want %q", k, got, k+"="+want)
		}
	}
}

func TestConfigLoadedButNotInAttachArgv(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g history-limit 12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Config = conf
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_conf", Dir: t.TempDir()}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	args := append(append([]string{}, c.Socket...), "show-options", "-gv", "history-limit")
	out, err := exec.Command(c.Bin, args...).Output()
	if err != nil {
		t.Fatalf("show-options: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "12345" {
		t.Fatalf("history-limit = %q, want 12345 from the config file", got)
	}

	want := append(append([]string{c.Bin, "-u"}, c.Socket...), "attach-session", "-t", "=s_conf")
	if got := c.AttachArgv("s_conf"); !reflect.DeepEqual(got, want) {
		t.Fatalf("AttachArgv = %v, want %v (the server is already running, so no -f)", got, want)
	}
}

func TestListSessionsUTF8WithoutLocale(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	for _, k := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	const title = "émoji 🐕"
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    "s_utf8",
		Dir:     t.TempDir(),
		Options: map[string]string{"@togo_name": title},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sessions, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != title {
		t.Fatalf("ListSessions = %+v, want one session titled %q", sessions, title)
	}
}

// TestTrailingSemicolonRoundTrips covers tmux's argv parsing, which treats
// any argument ending in ";" as a command separator.
func TestTrailingSemicolonRoundTrips(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "d;")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const name = "api;"
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    name,
		Dir:     dir,
		Env:     map[string]string{"K": "v;"},
		Options: map[string]string{"@togo_name": ";", "@togo_agent": `a\;`},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sessions, err := c.ListSessions(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions = %+v, %v; want one session", sessions, err)
	}
	s := sessions[0]
	wantDir, _ := filepath.EvalSymlinks(dir)
	if s.Name != name || s.Path != wantDir || s.Title != ";" || s.Agent != `a\;` {
		t.Fatalf("session = %+v, want name %q, path %q, title %q, agent %q", s, name, wantDir, ";", `a\;`)
	}
	args := append(append([]string{}, c.Socket...), "show-environment", "-t", `=api\;`, "K")
	if out, err := exec.Command(c.Bin, args...).Output(); err != nil || strings.TrimSpace(string(out)) != "K=v;" {
		t.Errorf("show-environment K = %q, %v; want K=v;", out, err)
	}

	if err := c.SetOption(ctx, name, "@togo_name", "x;"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	if sessions, _ := c.ListSessions(ctx); len(sessions) != 1 || sessions[0].Title != "x;" {
		t.Errorf("Title after SetOption = %+v, want x;", sessions)
	}
	if got := c.AttachArgv(name); got[len(got)-1] != `=api\;` {
		t.Errorf("AttachArgv(%q) target = %q, want the escaped form", name, got[len(got)-1])
	}
	if err := c.KillSession(ctx, name); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if sessions, _ := c.ListSessions(ctx); len(sessions) != 0 {
		t.Fatalf("ListSessions after kill = %+v, want none", sessions)
	}
}

func TestNewSessionSetsOptionsBeforePaneCanExit(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    "s_quick",
		Dir:     t.TempDir(),
		Command: []string{"/usr/bin/true"},
		Options: map[string]string{"@togo_bib": "01", "@togo_name": "n"},
	})
	if err != nil {
		t.Fatalf("NewSession with a command that exits at once: %v", err)
	}
}

func TestNewSessionRejectsSeparatorsInOptions(t *testing.T) {
	c := tmuxtest.New(t)
	err := c.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:    "s_bad",
		Dir:     t.TempDir(),
		Options: map[string]string{"@togo_name": "a\x1fb"},
	})
	if err == nil {
		t.Fatal("NewSession with a separator in an option value = nil error")
	}
	if sessions, _ := c.ListSessions(context.Background()); len(sessions) != 0 {
		t.Fatalf("ListSessions = %+v, want no session created", sessions)
	}
}
