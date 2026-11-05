package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

var managedID = regexp.MustCompile(`^s_[a-z2-7]{10}$`)

func TestCreateListKill(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	dir := t.TempDir()

	got := mustCreate(t, s, session.CreateOpts{Agent: "shell", Cwd: dir})
	if !managedID.MatchString(got.ID) {
		t.Errorf("ID = %q, want a managed id", got.ID)
	}
	if got.Bib == nil || *got.Bib != "01" {
		t.Errorf("Bib = %v, want 01", got.Bib)
	}
	if got.Name != "Shell" || got.Agent != "shell" || got.Status != "idle" || !got.Managed {
		t.Errorf("Create = %+v, want name Shell, agent shell, status idle, managed", got)
	}
	if got.Cwd != realpath(t, dir) {
		t.Errorf("Cwd = %q, want %q", got.Cwd, realpath(t, dir))
	}
	if !got.CreatedAt.Equal(fixedNow) || got.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want %v in UTC", got.CreatedAt, fixedNow)
	}
	if got.Activity != nil || got.AttachedClients != 0 {
		t.Errorf("Activity = %v, AttachedClients = %d; want nil, 0", got.Activity, got.AttachedClients)
	}

	if list := mustList(t, s); !reflect.DeepEqual(list, []session.Session{got}) {
		t.Fatalf("List = %+v, want [%+v]", list, got)
	}
	if recent, err := s.Recent.List(); err != nil || !reflect.DeepEqual(recent, []string{dir}) {
		t.Errorf("Recent = %v, %v; want [%s]", recent, err, dir)
	}
	for k, want := range map[string]string{"TOGO_SESSION": got.ID, "TOGO_BIB": "01"} {
		if env := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+got.ID, k); env != k+"="+want {
			t.Errorf("session env %s = %q, want %q", k, env, k+"="+want)
		}
	}

	if err := s.Kill(ctx, got.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if list := mustList(t, s); len(list) != 0 {
		t.Fatalf("List after Kill = %+v, want empty", list)
	}
	if err := s.Kill(ctx, got.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("second Kill error = %v, want ErrNotFound", err)
	}
}

func TestCreateDefaultsAndSize(t *testing.T) {
	s := newService(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := s.Create(context.Background(), session.CreateOpts{})
	if err != nil {
		t.Fatalf("Create with defaults: %v", err)
	}
	if got.Agent != "shell" || got.Name != "Shell" || got.Cwd != realpath(t, home) {
		t.Errorf("Create defaults = %+v, want agent shell, name Shell, cwd %s", got, home)
	}
	if size := tmuxOut(t, s.Tmux, "display-message", "-p", "-t", "="+got.ID+":", "#{window_width}x#{window_height}"); size != "80x24" {
		t.Errorf("default size = %s, want 80x24", size)
	}

	sized := mustCreate(t, s, session.CreateOpts{Cwd: "~", Cols: 100, Rows: 30})
	if size := tmuxOut(t, s.Tmux, "display-message", "-p", "-t", "="+sized.ID+":", "#{window_width}x#{window_height}"); size != "100x30" {
		t.Errorf("size = %s, want 100x30", size)
	}
}

func TestCreateRejectsBadParams(t *testing.T) {
	s := newService(t)
	dir := t.TempDir()
	cases := []session.CreateOpts{
		{Agent: "nope", Cwd: dir},
		{Cwd: dir, Cols: 19},
		{Cwd: dir, Cols: 501},
		{Cwd: dir, Rows: 4},
		{Cwd: dir, Rows: 301},
		{Cwd: dir, Cols: -1},
	}
	for _, o := range cases {
		var ipe *session.InvalidParamsError
		if _, err := s.Create(context.Background(), o); !errors.As(err, &ipe) {
			t.Errorf("Create(%+v) error = %v, want *InvalidParamsError", o, err)
		}
	}
	if _, err := s.Create(context.Background(), session.CreateOpts{Cwd: "relative"}); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("Create with a relative cwd: error = %v, want fsx.ErrInvalidPath", err)
	}
	if list := mustList(t, s); len(list) != 0 {
		t.Fatalf("List after rejected creates = %+v, want empty", list)
	}
}

func TestBibReuse(t *testing.T) {
	s := newService(t)
	var ids []string
	for i := 1; i <= 3; i++ {
		got := mustCreate(t, s, session.CreateOpts{})
		if want := fmt.Sprintf("%02d", i); *got.Bib != want {
			t.Fatalf("create %d: Bib = %s, want %s", i, *got.Bib, want)
		}
		ids = append(ids, got.ID)
	}
	if err := s.Kill(context.Background(), ids[1]); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if got := mustCreate(t, s, session.CreateOpts{}); *got.Bib != "02" {
		t.Fatalf("Bib after freeing 02 = %s, want 02", *got.Bib)
	}
}

func TestCreateHostileNames(t *testing.T) {
	s := newService(t)
	// The macOS temp dir is too long to fit in a 64-rune name.
	short, err := os.MkdirTemp("/tmp", "togo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	marker := filepath.Join(short, "pwn")
	names := []string{
		`a "b" 'c'`,
		fmt.Sprintf("x; $(touch %s)", marker),
		"`touch " + marker + "`",
		"api;",
		";",
		`a\\;`,
		"émoji 🐕",
		strings.Repeat("🐕", 64),
	}
	for _, name := range names {
		got := mustCreate(t, s, session.CreateOpts{Name: "  " + name + " "})
		if got.Name != name {
			t.Errorf("Create name = %q, want %q", got.Name, name)
		}
	}
	listed := map[string]bool{}
	for _, sess := range mustList(t, s) {
		listed[sess.Name] = true
	}
	for _, name := range names {
		if !listed[name] {
			t.Errorf("List is missing name %q", name)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("marker %s exists (err %v): a name reached a shell", marker, err)
	}

	for _, bad := range []string{"a\nb", "a\tb", "a\x1fb", "a\u0085b", strings.Repeat("x", 65), "\xff"} {
		var ipe *session.InvalidParamsError
		if _, err := s.Create(context.Background(), session.CreateOpts{Cwd: t.TempDir(), Name: bad}); !errors.As(err, &ipe) {
			t.Errorf("Create(name %q) error = %v, want *InvalidParamsError", bad, err)
		}
	}
	semiDir := filepath.Join(t.TempDir(), "d;")
	if err := os.Mkdir(semiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := mustCreate(t, s, session.CreateOpts{Cwd: semiDir}); got.Cwd != realpath(t, semiDir) {
		t.Errorf("Cwd = %q, want %q", got.Cwd, realpath(t, semiDir))
	}
	if recent, _ := s.Recent.List(); len(recent) == 0 || recent[0] != semiDir {
		t.Errorf("Recent = %v, want %q first", recent, semiDir)
	}
	if got := mustCreate(t, s, session.CreateOpts{Name: " \t "}); got.Name != "Shell" {
		t.Errorf("whitespace-only name became %q, want the agent name Shell", got.Name)
	}
}

func TestRename(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	created := mustCreate(t, s, session.CreateOpts{})

	got, err := s.Rename(ctx, created.ID, "  api server ")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	want := created
	want.Name = "api server"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Rename = %+v, want %+v", got, want)
	}
	if list := mustList(t, s); list[0].Name != "api server" {
		t.Fatalf("List after Rename has name %q", list[0].Name)
	}

	var ipe *session.InvalidParamsError
	if _, err := s.Rename(ctx, created.ID, "  "); !errors.As(err, &ipe) {
		t.Errorf("Rename to blank: error = %v, want *InvalidParamsError", err)
	}
	if _, err := s.Rename(ctx, "not-an-id", "x"); !errors.As(err, &ipe) {
		t.Errorf("Rename bad id: error = %v, want *InvalidParamsError", err)
	}
	if _, err := s.Rename(ctx, "s_aaaaaaaaaa", "x"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Rename unknown id: error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentCreateUniqueBibs(t *testing.T) {
	a := newService(t)
	b := &session.Service{
		Tmux:     &tmux.Client{Bin: a.Tmux.Bin, Socket: a.Tmux.Socket},
		Recent:   &state.Recent{File: a.Recent.File},
		LockFile: a.LockFile,
		Shell:    a.Shell,
	}
	services := []*session.Service{a, b}

	var mu sync.Mutex
	bibs := map[string]bool{}
	var wg sync.WaitGroup
	for _, s := range services {
		for range 10 {
			wg.Go(func() {
				got, err := s.Create(context.Background(), session.CreateOpts{Cwd: os.TempDir()})
				if err != nil {
					t.Errorf("Create: %v", err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if bibs[*got.Bib] {
					t.Errorf("bib %s handed out twice", *got.Bib)
				}
				bibs[*got.Bib] = true
			})
		}
	}
	wg.Wait()
	if len(bibs) != 20 {
		t.Fatalf("got %d unique bibs, want 20", len(bibs))
	}
}

func TestUnmanagedListedReadOnly(t *testing.T) {
	s := newService(t)
	s.User = tmuxtest.New(t)
	ctx := context.Background()
	if err := s.User.NewSession(ctx, tmux.NewSessionOpts{Name: "my work", Dir: t.TempDir()}); err != nil {
		t.Fatalf("NewSession on user socket: %v", err)
	}
	managed := mustCreate(t, s, session.CreateOpts{})

	list := mustList(t, s)
	if len(list) != 2 || list[0].ID != managed.ID {
		t.Fatalf("List = %+v, want the managed session then the unmanaged one", list)
	}
	u := list[1]
	if u.ID != "u_6d7920776f726b" || u.Bib != nil || u.Name != "my work" || u.Agent != "shell" || u.Managed || u.Status != "idle" {
		t.Fatalf("unmanaged session = %+v", u)
	}

	if err := s.Kill(ctx, u.ID); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("Kill unmanaged: error = %v, want ErrNotManaged", err)
	}
	if _, err := s.Rename(ctx, u.ID, "x"); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("Rename unmanaged: error = %v, want ErrNotManaged", err)
	}
	argv, err := s.AttachCommand(ctx, u.ID)
	if err != nil {
		t.Fatalf("AttachCommand unmanaged: %v", err)
	}
	if want := s.User.AttachArgv("my work"); !reflect.DeepEqual(argv, want) {
		t.Errorf("AttachCommand = %v, want %v", argv, want)
	}
	if sessions, _ := s.User.ListSessions(ctx); len(sessions) != 1 || sessions[0].Name != "my work" {
		t.Errorf("user socket sessions = %+v, want my work untouched", sessions)
	}
	if _, err := s.AttachCommand(ctx, "u_6e6f7065"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("AttachCommand unknown unmanaged: error = %v, want ErrNotFound", err)
	}
}

func TestAttachCommandManaged(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	got := mustCreate(t, s, session.CreateOpts{})
	argv, err := s.AttachCommand(ctx, got.ID)
	if err != nil {
		t.Fatalf("AttachCommand: %v", err)
	}
	if want := s.Tmux.AttachArgv(got.ID); !reflect.DeepEqual(argv, want) {
		t.Errorf("AttachCommand = %v, want %v", argv, want)
	}
	if _, err := s.AttachCommand(ctx, "s_aaaaaaaaaa"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("AttachCommand unknown: error = %v, want ErrNotFound", err)
	}
}

func TestPhoneAttachCommandMarksManagedSession(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	got := mustCreate(t, s, session.CreateOpts{})
	argv, err := s.PhoneAttachCommand(ctx, got.ID)
	if err != nil {
		t.Fatalf("PhoneAttachCommand: %v", err)
	}
	if want := s.Tmux.AttachArgv(got.ID); !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
	if out := tmuxOut(t, s.Tmux, "show-environment", "-t", "="+got.ID, "TOGO_CLIENT"); out != "TOGO_CLIENT=1" {
		t.Errorf("session environment = %q, want TOGO_CLIENT=1", out)
	}
	if _, err := s.PhoneAttachCommand(ctx, "s_aaaaaaaaaa"); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("unknown session: error = %v, want ErrNotFound", err)
	}
}

func TestPhoneAttachCommandLeavesUnmanagedSessionAlone(t *testing.T) {
	s := newService(t)
	s.User = tmuxtest.New(t)
	ctx := context.Background()
	if err := s.User.NewSession(ctx, tmux.NewSessionOpts{Name: "my work", Dir: t.TempDir()}); err != nil {
		t.Fatalf("NewSession on user socket: %v", err)
	}
	argv, err := s.PhoneAttachCommand(ctx, "u_6d7920776f726b")
	if err != nil {
		t.Fatalf("PhoneAttachCommand: %v", err)
	}
	if want := s.User.AttachArgv("my work"); !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
	for _, c := range []*tmux.Client{s.User, s.Tmux} {
		out, _ := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "show-environment", "-g")...).CombinedOutput()
		if strings.Contains(string(out), "TOGO_CLIENT") {
			t.Errorf("TOGO_CLIENT set on %v: %s", c.Socket, out)
		}
	}
	if out, _ := exec.Command(s.User.Bin, append(append([]string{}, s.User.Socket...), "show-environment", "-t", "=my work", "TOGO_CLIENT")...).CombinedOutput(); strings.Contains(string(out), "TOGO_CLIENT=1") {
		t.Errorf("unmanaged session environment was changed: %s", out)
	}
}

func TestTmuxTooOld(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'tmux 2.9'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := newService(t)
	s.Tmux = &tmux.Client{Bin: bin, Socket: []string{"-L", "unused"}}

	_, err := s.List(context.Background())
	var tue *session.TmuxUnavailableError
	if !errors.As(err, &tue) || tue.Found != "2.9" {
		t.Fatalf("List error = %v, want *TmuxUnavailableError{Found: 2.9}", err)
	}
	if _, err := s.Create(context.Background(), session.CreateOpts{Cwd: t.TempDir()}); !errors.As(err, &tue) {
		t.Fatalf("Create error = %v, want *TmuxUnavailableError", err)
	}
}

func TestTmuxMissing(t *testing.T) {
	s := newService(t)
	s.Tmux = &tmux.Client{Bin: "/nonexistent/tmux"}
	_, err := s.List(context.Background())
	var tue *session.TmuxUnavailableError
	if !errors.As(err, &tue) || tue.Found != "" {
		t.Fatalf("List error = %v, want *TmuxUnavailableError with empty Found", err)
	}
}

func TestResolveBibOrID(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	got := mustCreate(t, s, session.CreateOpts{})
	for _, in := range []string{"1", "01", got.ID} {
		r, err := s.Resolve(ctx, in)
		if err != nil || r.ID != got.ID {
			t.Errorf("Resolve(%q) = %+v, %v; want session %s", in, r, err, got.ID)
		}
	}
	for _, in := range []string{"77", "s_aaaaaaaaaa", "u_6e6f7065"} {
		if _, err := s.Resolve(ctx, in); !errors.Is(err, session.ErrNotFound) {
			t.Errorf("Resolve(%q) error = %v, want ErrNotFound", in, err)
		}
	}
	var ipe *session.InvalidParamsError
	for _, in := range []string{"0", "100", "abc", "", "+1", "-1", " 1", "001"} {
		if _, err := s.Resolve(ctx, in); !errors.As(err, &ipe) {
			t.Errorf("Resolve(%q) error = %v, want *InvalidParamsError", in, err)
		}
	}
}

func TestListSurvivesBrokenUserServer(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'protocol version mismatch' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := newService(t)
	s.User = &tmux.Client{Bin: bin}
	got := mustCreate(t, s, session.CreateOpts{})

	list := mustList(t, s)
	if len(list) != 1 || list[0].ID != got.ID {
		t.Fatalf("List = %+v, want only the managed session", list)
	}
	if _, err := s.AttachCommand(context.Background(), "u_6d79"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("AttachCommand on an unmanaged id = %v, want ErrNotFound", err)
	}
}

func TestCreateWhenPaneExitsAtOnce(t *testing.T) {
	s := newService(t)
	// tmux starts each pane with the server's $SHELL; true exits at once,
	// and tmux destroys the session as soon as it notices.
	t.Setenv("SHELL", "/usr/bin/true")
	dir := t.TempDir()

	got, err := s.Create(context.Background(), session.CreateOpts{Cwd: dir, Name: "gone"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !managedID.MatchString(got.ID) || got.Bib == nil || *got.Bib != "01" || got.Name != "gone" ||
		got.Agent != "shell" || got.Cwd != realpath(t, dir) || !got.CreatedAt.Equal(fixedNow) || !got.Managed {
		t.Fatalf("Create = %+v, want the session Create made", got)
	}
	if got.Status != "exited" {
		t.Fatalf("Status = %q, want exited", got.Status)
	}
}

func TestCreateUsesServicePath(t *testing.T) {
	s := newService(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "omp"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.Path = bin

	got := mustCreate(t, s, session.CreateOpts{Agent: "omp"})
	if got.Agent != "omp" || got.Name != "OMP" {
		t.Fatalf("Create omp = %+v", got)
	}
	var ipe *session.InvalidParamsError
	if _, err := s.Create(context.Background(), session.CreateOpts{Agent: "codex", Cwd: t.TempDir()}); !errors.As(err, &ipe) {
		t.Fatalf("Create codex with codex absent from Path: error = %v, want *InvalidParamsError", err)
	}
}

// newPrinting creates a managed-looking session cols wide and 24 high on s.Tmux whose pane
// runs script and then idles, so the output stays on screen.
func newPrinting(t *testing.T, c *tmux.Client, id string, cols int, script string) {
	t.Helper()
	err := c.NewSession(context.Background(), tmux.NewSessionOpts{
		Name: id, Dir: t.TempDir(), Cols: cols, Rows: 24,
		Command: []string{"/bin/sh", "-c", script + "; sleep 300"},
		Options: map[string]string{"@togo_bib": "01"},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
}

func waitCapture(t *testing.T, s *session.Service, id, want string, lines int) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		text, truncated, err := s.Capture(context.Background(), id, lines, true)
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if strings.Contains(text, want) || time.Now().After(deadline) {
			return text, truncated
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCaptureLinesRange(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, n := range []int{0, -1, 10001} {
		var invalid *session.InvalidParamsError
		if _, _, err := s.Capture(ctx, "s_abcdefghij", n, true); !errors.As(err, &invalid) {
			t.Errorf("Capture lines=%d: error = %v, want *InvalidParamsError", n, err)
		}
	}
	var invalid *session.InvalidParamsError
	if _, _, err := s.Capture(ctx, "bogus", 10, true); !errors.As(err, &invalid) {
		t.Errorf("Capture malformed id: error = %v, want *InvalidParamsError", err)
	}
	if _, _, err := s.Capture(ctx, "s_abcdefghij", 10, true); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Capture unknown id: error = %v, want ErrNotFound", err)
	}
}

func TestCaptureReturnsHistoryAndScreen(t *testing.T) {
	s := newService(t)
	newPrinting(t, s.Tmux, "s_abcdefghij", 80, `seq 1 100`)
	text, truncated := waitCapture(t, s, "s_abcdefghij", "\n100", 1000)
	if truncated || !strings.HasPrefix(text, "1\n") || !strings.HasSuffix(text, "\n100") {
		t.Fatalf("Capture = %.20q...%q truncated=%v, want 1..100 with no trailing blank rows", text, text[max(0, len(text)-10):], truncated)
	}
	if strings.Contains(text, "\x1b") {
		t.Error("Capture contains escape sequences, want plain text")
	}
	few, _ := waitCapture(t, s, "s_abcdefghij", "\n100", 10)
	if lines := strings.Split(few, "\n"); len(lines) > 10+24 || lines[len(lines)-1] != "100" {
		t.Errorf("Capture lines=10 returned %d lines ending %q, want at most 34 ending at 100", len(lines), lines[len(lines)-1])
	}
}

func TestCaptureTruncates(t *testing.T) {
	s := newService(t)
	dir := t.TempDir()
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g history-limit 20000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Tmux.Config = conf
	newPrinting(t, s.Tmux, "s_abcdefghij", 200,
		`awk 'BEGIN{pad=sprintf("%94s",""); gsub(/ /,"x",pad); for(i=1;i<=12000;i++) printf "L%05d %s\n", i, pad}'`)
	text, truncated := waitCapture(t, s, "s_abcdefghij", "L12000", 10000)
	if !truncated {
		t.Error("truncated = false, want true for 1.2 MB of output")
	}
	if len(text) > 900000 {
		t.Errorf("len(text) = %d, want at most 900000", len(text))
	}
	if !strings.HasSuffix(text, "L12000 "+strings.Repeat("x", 94)) {
		t.Errorf("text does not end with the newest line")
	}
	if !strings.HasPrefix(text, "L") || len(strings.SplitN(text, "\n", 2)[0]) != 101 {
		t.Errorf("text starts mid-line: %.30q", text)
	}
}

func TestCaptureUnmanaged(t *testing.T) {
	s := newService(t)
	s.User = tmuxtest.New(t)
	newPrinting(t, s.User, "my work", 80, `echo from-user-session`)
	text, _ := waitCapture(t, s, "u_6d7920776f726b", "from-user-session", 100)
	if !strings.Contains(text, "from-user-session") {
		t.Fatalf("Capture of unmanaged session = %q", text)
	}
	if _, _, err := s.Capture(context.Background(), "u_6e6f7065", 10, true); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Capture unknown unmanaged: error = %v, want ErrNotFound", err)
	}
}

// The frame limit applies to the encoded reply, and json.Marshal turns
// "<", ">" and "&" into 6 bytes each, so dense markup must be cut earlier
// than plain text.
func TestCaptureFitsFrameWhenEscapeHeavy(t *testing.T) {
	s := newService(t)
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g history-limit 20000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Tmux.Config = conf
	newPrinting(t, s.Tmux, "s_abcdefghij", 200,
		`awk 'BEGIN{for(i=1;i<=12000;i++) printf "%05d <a href=\"x\">&</a>\\\\ <b>&&<b>&&<b>&&<b>&&\n", i}'`)
	text, truncated := waitCapture(t, s, "s_abcdefghij", "12000 <a", 10000)
	reply, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"text": text, "truncated": truncated}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply) > rpc.MaxFrame {
		t.Errorf("encoded reply is %d bytes, want at most %d", len(reply), rpc.MaxFrame)
	}
	if !truncated {
		t.Error("truncated = false, want true")
	}
	if !strings.HasSuffix(text, `12000 <a href="x">&</a>\\ <b>&&<b>&&<b>&&<b>&&`) {
		t.Errorf("text does not end with the newest line: %.60q", text[max(0, len(text)-60):])
	}
}
