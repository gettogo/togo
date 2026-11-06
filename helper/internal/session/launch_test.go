package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// fakeBin returns a directory holding an executable for each name that
// waits for a line of input, as an agent waits at its prompt, and puts it
// first on PATH so the panes of sessions created afterwards find them.
func fakeBin(t *testing.T, names ...string) string {
	t.Helper()
	dir := realpath(t, t.TempDir())
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nread -r line\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+os.Getenv("PATH"))
	return dir
}

func reason(t *testing.T, err error) string {
	t.Helper()
	var ipe *session.InvalidParamsError
	if !errors.As(err, &ipe) {
		t.Fatalf("error = %v, want *InvalidParamsError", err)
	}
	return ipe.Reason
}

func echoProgram(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "argv-echo.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLaunchArgsReachProgramLiterally(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	args := []string{";", "$(touch X)", "`id`", "#{session_name}", "'q'", "\"d\"", "a;", "-x", "café", "{", "}", "\\;", "~", "*"}
	mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: echoProgram(t), Args: args, Cwd: dir})

	out := filepath.Join(dir, "argv.out")
	var got []byte
	for deadline := time.Now().Add(10 * time.Second); ; {
		b, err := os.ReadFile(out)
		if err == nil {
			got = b
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("program never wrote %s: %v", out, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var want bytes.Buffer
	for _, a := range args {
		want.WriteString(a)
		want.WriteByte(0)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("argv = %q\nwant   %q", got, want.Bytes())
	}
	if _, err := os.Stat(filepath.Join(dir, "X")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an argument was run as a command: X exists (err %v)", err)
	}
}

func TestLaunchRejectsBadProgram(t *testing.T) {
	tmp := realpath(t, t.TempDir())
	plain := filepath.Join(tmp, "plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(tmp, "dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(sub, link); err != nil {
		t.Fatal(err)
	}
	path := fakeBin(t)
	for _, prog := range []string{
		"../x", "-rf", "bin/claude", sub, link, plain, "/usr/bin/../bin/true", "relative",
		"/" + strings.Repeat("a", wire.MaxProgramPath), "/tmp/a\nb", "na\x00me", "",
	} {
		_, _, err := session.ValidateLaunch(wire.AgentCustom, prog, nil, path)
		if prog == "relative" {
			// A bare name that matches the pattern but is not on PATH.
			if r := reason(t, err); r != wire.ReasonNotInstalled {
				t.Errorf("program %q: reason %q, want %q", prog, r, wire.ReasonNotInstalled)
			}
			continue
		}
		if r := reason(t, err); r != wire.ReasonProgram {
			t.Errorf("program %q: reason %q, want %q", prog, r, wire.ReasonProgram)
		}
	}
	if _, _, err := session.ValidateLaunch(wire.AgentCustom, "nosuchprogram", nil, path); reason(t, err) != wire.ReasonNotInstalled {
		t.Errorf("unknown bare name: reason %q, want not_installed", reason(t, err))
	}
	if _, _, err := session.ValidateLaunch("claude", "", nil, path); reason(t, err) != wire.ReasonNotInstalled {
		t.Errorf("claude absent from PATH: reason %q, want not_installed", reason(t, err))
	}
}

func TestLaunchAcceptsGoodPrograms(t *testing.T) {
	path := fakeBin(t, "aider", "claude")
	a, l, err := session.ValidateLaunch(wire.AgentCustom, "aider", []string{"--yes"}, path)
	if err != nil || a.ID != wire.AgentCustom || !reflect.DeepEqual(l.Argv(), []string{"aider", "--yes"}) {
		t.Fatalf("custom aider = %+v %+v %v", a, l, err)
	}
	abs := echoProgram(t)
	if _, l, err := session.ValidateLaunch(wire.AgentCustom, abs, nil, ""); err != nil || !reflect.DeepEqual(l.Argv(), []string{abs}) {
		t.Fatalf("absolute program = %+v %v", l, err)
	}
	a, l, err = session.ValidateLaunch("claude", "", []string{"--dangerously-skip-permissions"}, path)
	if err != nil || a.ID != "claude" || !reflect.DeepEqual(l.Argv(), []string{"claude", "--dangerously-skip-permissions"}) {
		t.Fatalf("claude = %+v %+v %v", a, l, err)
	}
	a, l, err = session.ValidateLaunch("", "", nil, "")
	if err != nil || a.ID != "shell" || l.Argv() != nil {
		t.Fatalf("default = %+v %+v %v", a, l, err)
	}
}

func TestLaunchRejectsBadArgs(t *testing.T) {
	path := fakeBin(t, "claude")
	many := make([]string, wire.MaxArgs+1)
	for i := range many {
		many[i] = "-v"
	}
	total := []string{
		strings.Repeat("a", wire.MaxArgBytes), strings.Repeat("a", wire.MaxArgBytes),
		strings.Repeat("a", wire.MaxArgBytes), strings.Repeat("a", wire.MaxArgBytes), "a",
	}
	for name, args := range map[string][]string{
		"65 entries":   many,
		"4097 bytes":   {strings.Repeat("a", wire.MaxArgBytes+1)},
		"newline":      {"a\nb"},
		"tab":          {"a\tb"},
		"nul":          {"a\x00b"},
		"escape":       {"a\x1bb"},
		"c1 control":   {"a\u0085b"},
		"invalid utf8": {"a\xffb"},
		"empty":        {""},
		"16385 total":  total,
	} {
		if _, _, err := session.ValidateLaunch("claude", "", args, path); reason(t, err) != wire.ReasonArgs {
			t.Errorf("%s: reason %q, want args", name, reason(t, err))
		}
	}
	if _, _, err := session.ValidateLaunch("shell", "", []string{"-x"}, path); reason(t, err) != wire.ReasonArgs {
		t.Errorf("shell with args: reason %q, want args", reason(t, err))
	}
	if _, _, err := session.ValidateLaunch("", "", []string{"-x"}, path); reason(t, err) != wire.ReasonArgs {
		t.Errorf("default shell with args: reason %q, want args", reason(t, err))
	}
	if _, _, err := session.ValidateLaunch("claude", "", total[:4], path); err != nil {
		t.Errorf("16384 bytes in total: %v", err)
	}
	if _, _, err := session.ValidateLaunch("claude", "", many[:wire.MaxArgs], path); err != nil {
		t.Errorf("64 entries: %v", err)
	}
}

func TestProgramOnlyForCustom(t *testing.T) {
	path := fakeBin(t, "claude", "aider")
	if _, _, err := session.ValidateLaunch("claude", "aider", nil, path); reason(t, err) != wire.ReasonProgram {
		t.Errorf("program with claude: reason %q, want program", reason(t, err))
	}
	if _, _, err := session.ValidateLaunch("shell", "aider", nil, path); reason(t, err) != wire.ReasonProgram {
		t.Errorf("program with shell: reason %q, want program", reason(t, err))
	}
	if _, _, err := session.ValidateLaunch(wire.AgentCustom, "", nil, path); reason(t, err) != wire.ReasonProgram {
		t.Errorf("custom without program: reason %q, want program", reason(t, err))
	}
}

func TestWrapperForEachShell(t *testing.T) {
	const posix = `t=$1; shift; "$@"; s=$?; [ -n "$t" ] && "$t" session-exit "$s" 2>/dev/null; exec "${SHELL:-/bin/sh}" -l`
	const fish = `set -l t $argv[1]; command $argv[2..-1]; set -l s $status; test -n "$t"; and $t session-exit $s 2>/dev/null; exec $SHELL -l`
	if session.PosixWrapper != posix || session.FishWrapper != fish {
		t.Fatalf("wrapper text changed:\n%s\n%s", session.PosixWrapper, session.FishWrapper)
	}
	l := session.Launch{Program: "claude", Args: []string{"--model", "opus"}}
	const togo = "/opt/to go/togo"
	for shell, want := range map[string][]string{
		"/bin/zsh":               {"/bin/zsh", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/usr/local/bin/bash":    {"/usr/local/bin/bash", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/bin/sh":                {"/bin/sh", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/opt/homebrew/bin/fish": {"/opt/homebrew/bin/fish", "-lic", fish, togo, "claude", "--model", "opus"},
		"/bin/tcsh":              {"/bin/sh", "-lc", posix, "togo", togo, "claude", "--model", "opus"},
		"/usr/bin/nu":            {"/bin/sh", "-lc", posix, "togo", togo, "claude", "--model", "opus"},
		"/usr/local/bin/mksh":    {"/usr/local/bin/mksh", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/run/current/bin/dash":  {"/run/current/bin/dash", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/bin/ksh":               {"/bin/ksh", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/usr/bin/yash":          {"/usr/bin/yash", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
		"/bin/ash":               {"/bin/ash", "-lic", posix, "togo", togo, "claude", "--model", "opus"},
	} {
		if got := session.LaunchWrapper(shell, togo, l); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q\nwant %q", shell, got, want)
		}
	}
	if got := session.LaunchWrapper("/bin/zsh", togo, session.Launch{}); got != nil {
		t.Errorf("plain shell = %q, want nil", got)
	}
}

func TestCreateStoresArgvOption(t *testing.T) {
	s := newService(t)
	s.Path = fakeBin(t, "claude")
	c := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}})
	sh := mustCreate(t, s, session.CreateOpts{})
	if !reflect.DeepEqual(c.Command, []string{"claude", "--model", "opus"}) {
		t.Errorf("Create command = %q", c.Command)
	}
	byID := map[string]session.Session{}
	for _, x := range mustList(t, s) {
		byID[x.ID] = x
	}
	if got := byID[c.ID].Command; !reflect.DeepEqual(got, []string{"claude", "--model", "opus"}) {
		t.Errorf("listed command = %q", got)
	}
	if got := byID[sh.ID].Command; got != nil {
		t.Errorf("shell command = %q, want nil", got)
	}
	if v := tmuxOut(t, s.Tmux, "show-options", "-v", "-t", c.ID, session.OptionArgv); v != `["claude","--model","opus"]` {
		t.Errorf("%s = %s", session.OptionArgv, v)
	}
	b, err := json.Marshal(byID[sh.ID])
	if err != nil || !strings.Contains(string(b), `"command":null`) || !strings.Contains(string(b), `"exit":null`) {
		t.Errorf("shell JSON %s (err %v), want command and exit null", b, err)
	}
}

func TestCreateCustomNameDefaultsToProgramBase(t *testing.T) {
	s := newService(t)
	s.Path = fakeBin(t, "aider")
	got := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: "aider", Args: []string{"--no-auto-commits"}})
	if got.Name != "aider" || got.Agent != wire.AgentCustom || !reflect.DeepEqual(got.Command, []string{"aider", "--no-auto-commits"}) {
		t.Errorf("custom aider = %+v", got)
	}
	named := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: echoProgram(t), Name: "mine"})
	if named.Name != "mine" {
		t.Errorf("explicit name = %q", named.Name)
	}
	abs := mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: echoProgram(t)})
	if abs.Name != "argv-echo.sh" {
		t.Errorf("absolute program name = %q, want argv-echo.sh", abs.Name)
	}
}

func TestCreateRejectsBadLaunchWithReason(t *testing.T) {
	s := newService(t)
	s.Path = fakeBin(t, "claude")
	_, err := s.Create(context.Background(), session.CreateOpts{Agent: "claude", Args: []string{"a\x00b"}, Cwd: t.TempDir()})
	if reason(t, err) != wire.ReasonArgs {
		t.Fatalf("reason %q, want args", reason(t, err))
	}
	if n := len(mustList(t, s)); n != 0 {
		t.Fatalf("%d sessions after a rejected create", n)
	}
}

func TestCheckProgram(t *testing.T) {
	s := newService(t)
	s.Path = "relative" + string(filepath.ListSeparator) + fakeBin(t, "aider")
	ctx := context.Background()
	path, ok, err := s.CheckProgram(ctx, "aider")
	if err != nil || !ok || path != filepath.Join(strings.Split(s.Path, string(filepath.ListSeparator))[1], "aider") {
		t.Errorf("aider = %q %v %v", path, ok, err)
	}
	if path, ok, err := s.CheckProgram(ctx, "nosuchprogram"); err != nil || ok || path != "" {
		t.Errorf("missing = %q %v %v", path, ok, err)
	}
	abs := echoProgram(t)
	if path, ok, err := s.CheckProgram(ctx, abs); err != nil || !ok || path != abs {
		t.Errorf("absolute = %q %v %v", path, ok, err)
	}
	if path, ok, err := s.CheckProgram(ctx, filepath.Join(t.TempDir(), "gone")); err != nil || ok || path != "" {
		t.Errorf("absolute missing = %q %v %v", path, ok, err)
	}
	for _, bad := range []string{"../x", "-rf", "bin/aider", "", "/usr/bin/../bin/true", t.TempDir()} {
		if _, _, err := s.CheckProgram(ctx, bad); reason(t, err) != wire.ReasonProgram {
			t.Errorf("%q: reason %q, want program", bad, reason(t, err))
		}
	}
}

// TestFixtures runs the launch and restore exchanges over the protocol:
// HOME holds proj, and bin with an executable claude and aider is the
// login PATH. The restore exchanges start with the record their fixtures
// describe.
func TestFixtures(t *testing.T) {
	names := []string{"session.create-args", "session.create-custom", "session.create-bad-args", "agents.check",
		"session.restorable", "session.restore", "session.dismiss"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			home := realpath(t, t.TempDir())
			if err := os.Mkdir(filepath.Join(home, "proj"), 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"claude", "aider"} {
				if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\nread -r line\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", home)
			svc := newService(t)
			t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
			svc.Path = bin
			svc.NewID = func() string { return "s_abcdefghij" }
			if !strings.HasPrefix(name, "session.create") && name != "agents.check" {
				seedRestorable(t, svc, filepath.Join(home, "proj"))
			}
			a := &api.API{Sessions: svc, Ext: map[string]api.Method{
				wire.MethodAgentsCheck:       api.AgentsCheck(svc),
				wire.MethodSessionRestorable: api.Restorable(svc),
				wire.MethodSessionRestore:    api.Restore(svc, nil),
				wire.MethodSessionDismiss:    api.Dismiss(svc),
			}}
			runFixture(t, a, home, name)
		})
	}
}

// TestWrapperReportsExitStatus runs the wrapper under each POSIX login
// shell on this machine with a stand-in helper that records its arguments.
func TestWrapperReportsExitStatus(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		t.Run(filepath.Base(shell), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s := newService(t)
			s.Shell = shell
			dir := realpath(t, t.TempDir())
			togo := filepath.Join(dir, "fake togo")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$(dirname \"$0\")/report.tmp\" && mv \"$(dirname \"$0\")/report.tmp\" \"$(dirname \"$0\")/report\"\n"
			if err := os.WriteFile(togo, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			s.TogoPath = togo
			mustCreate(t, s, session.CreateOpts{Agent: wire.AgentCustom, Program: "/bin/sh", Args: []string{"-c", "exit 3"}, Cwd: dir})
			report := filepath.Join(dir, "report")
			for deadline := time.Now().Add(10 * time.Second); ; {
				b, err := os.ReadFile(report)
				if err == nil {
					if string(b) != "session-exit\n3\n" {
						t.Fatalf("helper ran with %q, want session-exit 3", b)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("helper never ran: %v", err)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

// TestTestsNeverRunInstalledAgents puts a recording claude in a directory
// on the PATH the test started with, as an installed agent would be. A
// session whose claude passes validation through Service.Path alone must
// not find that one: its pane reports exit 127 and the installed claude
// never runs.
func TestTestsNeverRunInstalledAgents(t *testing.T) {
	installed := realpath(t, t.TempDir())
	ran := filepath.Join(installed, "ran")
	if err := os.WriteFile(filepath.Join(installed, "claude"), []byte("#!/bin/sh\ntouch "+ran+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", installed+string(filepath.ListSeparator)+os.Getenv("PATH"))

	s := newService(t)
	valid := realpath(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(valid, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.Path = valid
	dir := realpath(t, t.TempDir())
	report := filepath.Join(dir, "report")
	togo := filepath.Join(dir, "togo")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + report + ".tmp && mv " + report + ".tmp " + report + "\n"
	if err := os.WriteFile(togo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.TogoPath = togo
	mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}, Cwd: dir})
	for deadline := time.Now().Add(10 * time.Second); ; {
		b, err := os.ReadFile(report)
		if err == nil {
			if string(b) != "session-exit\n127\n" {
				t.Errorf("pane reported %q, want session-exit 127", b)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane never reported its exit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the installed claude ran (err %v)", err)
	}
}

// TestListIgnoresTamperedArgv rewrites a session's argv option by hand
// with values Create would never write: each is logged and the session is
// listed with no command.
func TestListIgnoresTamperedArgv(t *testing.T) {
	s := newService(t)
	s.Path = fakeBin(t, "claude")
	var logged []string
	s.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	c := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}})
	many, _ := json.Marshal(append([]string{"claude"}, make([]string, wire.MaxArgs+1)...))
	for _, v := range []string{
		"not json", "[]", `{"a":1}`, `["../x"]`, `["-rf"]`, `["/usr/bin/../bin/true"]`,
		`["claude","a\u0000b"]`, `["claude","a\nb"]`, `["claude",""]`, string(many),
		`["claude","` + strings.Repeat("a", wire.MaxArgBytes+1) + `"]`,
	} {
		logged = nil
		if out, err := exec.Command(s.Tmux.Bin, append(append([]string{}, s.Tmux.Socket...), "set-option", "-t", c.ID+":", session.OptionArgv, v)...).CombinedOutput(); err != nil {
			t.Fatalf("set-option %q: %v %s", v, err, out)
		}
		got := mustList(t, s)
		if len(got) != 1 || got[0].Command != nil {
			t.Errorf("%.40q: listed command %q, want nil", v, got[0].Command)
		}
		if len(logged) != 1 || !strings.Contains(logged[0], session.OptionArgv) || strings.Contains(logged[0], "../x") {
			t.Errorf("%.40q: logged %q, want one line naming the option without its value", v, logged)
		}
	}
}
