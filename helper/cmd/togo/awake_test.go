package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/awake"
)

type awakeFakes struct {
	home     string
	sudoers  string
	argvs    [][]string
	override *recordingOverride
}

type recordingOverride struct {
	calls []bool
	err   error
}

func (r *recordingOverride) Set(_ context.Context, on bool) error {
	r.calls = append(r.calls, on)
	return r.err
}

// fakeAwake points the awake command at a private TOGO_HOME, a sudoers path
// in a temp dir and runners that record instead of running sudo.
func fakeAwake(t *testing.T, goos string) *awakeFakes {
	t.Helper()
	f := &awakeFakes{
		home:     t.TempDir(),
		sudoers:  filepath.Join(t.TempDir(), "togo-awake"),
		override: &recordingOverride{},
	}
	t.Setenv("TOGO_HOME", f.home)
	oldAdmin, oldOverride, oldGOOS := awakeAdmin, awakeOverride, awakeGOOS
	t.Cleanup(func() { awakeAdmin, awakeOverride, awakeGOOS = oldAdmin, oldOverride, oldGOOS })
	awakeGOOS = goos
	awakeOverride = func() awake.SleepOverride { return f.override }
	awakeAdmin = func(home string, _ io.Writer) awake.Admin {
		run := func(_ context.Context, argv ...string) error {
			f.argvs = append(f.argvs, argv)
			if len(argv) > 2 && argv[1] == "/bin/mv" {
				return os.WriteFile(f.sudoers, []byte("rule"), 0o600)
			}
			if len(argv) > 2 && argv[1] == "/bin/rm" && argv[3] == f.sudoers {
				return os.Remove(f.sudoers)
			}
			return nil
		}
		return awake.Admin{Home: home, User: "alice", SudoersPath: f.sudoers, Run: run,
			Feed: func(ctx context.Context, _ string, argv ...string) error { return run(ctx, argv...) }}
	}
	return f
}

func runAwakeCmd(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runContext(context.Background(), append([]string{"awake"}, args...), nil, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestAwakeStatusWithoutDaemon(t *testing.T) {
	fakeAwake(t, "darwin")
	code, out, errOut := runAwakeCmd(t, "")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"Keep awake: plugged", "Battery floor: 20%", "daemon is not running"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAwakeOffAndFloorSavedWithoutDaemon(t *testing.T) {
	f := fakeAwake(t, "darwin")
	if code, _, errOut := runAwakeCmd(t, "", "off"); code != exitOK {
		t.Fatalf("off: exit %d: %s", code, errOut)
	}
	if code, _, errOut := runAwakeCmd(t, "", "--battery-floor", "30"); code != exitOK {
		t.Fatalf("floor: exit %d: %s", code, errOut)
	}
	s, err := awake.LoadSettings(f.home)
	if err != nil || s.Mode != awake.ModeOff || s.BatteryFloor != 30 {
		t.Fatalf("settings %+v %v", s, err)
	}
	if len(f.argvs) != 0 {
		t.Errorf("ran %q without a sudoers rule to remove", f.argvs)
	}
}

func TestAwakeRejectsBadArguments(t *testing.T) {
	fakeAwake(t, "darwin")
	for _, args := range [][]string{{"always"}, {"--battery-floor", "5"}, {"--battery-floor", "51"}, {"off", "plugged"}} {
		if code, _, _ := runAwakeCmd(t, "", args...); code != exitUsage {
			t.Errorf("%q: exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestAwakeBatteryConfirmsThenInstallsRule(t *testing.T) {
	f := fakeAwake(t, "darwin")
	code, out, errOut := runAwakeCmd(t, "y\n", "battery")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !strings.Contains(out, f.sudoers) || !strings.Contains(out, "alice ALL=(root) NOPASSWD: /usr/bin/pmset -a disablesleep 0, /usr/bin/pmset -a disablesleep 1") {
		t.Errorf("prompt does not show the path and rule:\n%s", out)
	}
	if len(f.argvs) != 6 || !slices.Equal(f.argvs[0][:2], []string{"/usr/bin/sudo", "/usr/bin/tee"}) ||
		!slices.Equal(f.argvs[5], []string{"/usr/bin/sudo", "-n", "/usr/bin/pmset", "-a", "disablesleep", "1"}) {
		t.Fatalf("ran %q, want the rule installed before disablesleep is set", f.argvs)
	}
	if s, _ := awake.LoadSettings(f.home); s.Mode != awake.ModeBattery {
		t.Fatalf("mode %s, want battery", s.Mode)
	}
	// No daemon runs, so nothing is active and the check's disablesleep 1
	// is undone at once.
	if !slices.Equal(f.override.calls, []bool{false}) {
		t.Fatalf("override calls %v, want one clear", f.override.calls)
	}
	if _, err := os.Stat(awake.OverrideMarker(f.home)); !os.IsNotExist(err) {
		t.Fatal("marker left after the clear")
	}
}

func TestAwakeBatteryDeclined(t *testing.T) {
	f := fakeAwake(t, "darwin")
	code, _, _ := runAwakeCmd(t, "n\n", "battery")
	if code != exitError || len(f.argvs) != 0 {
		t.Fatalf("exit %d, ran %q", code, f.argvs)
	}
	if s, _ := awake.LoadSettings(f.home); s.Mode != awake.ModePlugged {
		t.Fatalf("mode %s after declining", s.Mode)
	}
}

func TestAwakeBatteryWithRuleSkipsSetup(t *testing.T) {
	f := fakeAwake(t, "darwin")
	if err := os.WriteFile(f.sudoers, []byte("rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runAwakeCmd(t, "", "battery"); code != exitOK || len(f.argvs) != 0 {
		t.Fatalf("exit %d, ran %q: %s", code, f.argvs, errOut)
	}
}

func TestAwakeBatteryOnLinuxNeedsNoSudo(t *testing.T) {
	f := fakeAwake(t, "linux")
	if code, _, errOut := runAwakeCmd(t, "", "battery"); code != exitOK || len(f.argvs) != 0 || len(f.override.calls) != 0 {
		t.Fatalf("exit %d, ran %q, override %v: %s", code, f.argvs, f.override.calls, errOut)
	}
}

func TestAwakePluggedRemovesRule(t *testing.T) {
	f := fakeAwake(t, "darwin")
	if err := os.WriteFile(f.sudoers, []byte("rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runAwakeCmd(t, "", "plugged")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if want := []string{"/usr/bin/sudo", "/bin/rm", "-f", f.sudoers}; len(f.argvs) != 1 || !slices.Equal(f.argvs[0], want) {
		t.Fatalf("ran %q, want %q", f.argvs, want)
	}
	if !strings.Contains(out, "Removed "+f.sudoers) {
		t.Errorf("output does not report the removal:\n%s", out)
	}
}

func TestAwakeUninstallClearsAndRemoves(t *testing.T) {
	f := fakeAwake(t, "darwin")
	if err := os.WriteFile(f.sudoers, []byte("rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(awake.OverrideMarker(f.home), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := uninstallAwake(context.Background(), f.home, &out, &errb); code != exitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	want := [][]string{
		{"/usr/bin/sudo", "/usr/bin/pmset", "-a", "disablesleep", "0"},
		{"/usr/bin/sudo", "/bin/rm", "-f", f.sudoers},
	}
	if !slices.EqualFunc(f.argvs, want, slices.Equal) {
		t.Fatalf("ran %q, want %q", f.argvs, want)
	}
}

func TestAwakeBatteryRestoresWhenTheChangeFails(t *testing.T) {
	f := fakeAwake(t, "darwin")
	f.override.err = errors.New("sudo: a password is required")
	code, _, _ := runAwakeCmd(t, "y\n", "battery")
	if code != exitError {
		t.Fatalf("exit %d, want a failure", code)
	}
	last := f.argvs[len(f.argvs)-1]
	if want := []string{"/usr/bin/sudo", "/usr/bin/pmset", "-a", "disablesleep", "0"}; !slices.Equal(last, want) {
		t.Fatalf("last ran %q, want %q", last, want)
	}
	if _, err := os.Stat(awake.OverrideMarker(f.home)); !os.IsNotExist(err) {
		t.Fatal("marker left although disablesleep was restored")
	}
}

func TestAwakeOffClearsTogoSetSleepWithoutRule(t *testing.T) {
	f := fakeAwake(t, "darwin")
	if err := os.WriteFile(awake.OverrideMarker(f.home), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The unattended clear fails without a rule, so the command clears it
	// through a plain sudo.
	f.override.err = errors.New("sudo: a password is required")
	f.override.calls = nil
	code, _, errOut := runAwakeCmd(t, "", "off")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if want := []string{"/usr/bin/sudo", "/usr/bin/pmset", "-a", "disablesleep", "0"}; len(f.argvs) != 1 || !slices.Equal(f.argvs[0], want) {
		t.Fatalf("ran %q, want %q", f.argvs, want)
	}
}
