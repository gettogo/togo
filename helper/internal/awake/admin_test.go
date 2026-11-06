package awake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	argvs  [][]string
	inputs map[string]string
	fail   map[string]error
}

func (f *fakeRunner) run(_ context.Context, argv ...string) error {
	f.argvs = append(f.argvs, argv)
	return f.fail[strings.Join(argv, " ")]
}

func (f *fakeRunner) feed(ctx context.Context, input string, argv ...string) error {
	if f.inputs == nil {
		f.inputs = map[string]string{}
	}
	f.inputs[strings.Join(argv, " ")] = input
	return f.run(ctx, argv...)
}

func newAdmin(t *testing.T, fr *fakeRunner) Admin {
	return Admin{
		Home: t.TempDir(), User: "alice", SudoersPath: filepath.Join(t.TempDir(), "togo-awake"),
		Run: fr.run, Feed: fr.feed,
	}
}

func TestSudoersRuleIsNarrow(t *testing.T) {
	got, err := SudoersRule("alice")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Lets togo turn sleep off and on for \"togo awake battery\".\n" +
		"# Remove with: togo awake plugged\n" +
		"alice ALL=(root) NOPASSWD: /usr/bin/pmset -a disablesleep 0, /usr/bin/pmset -a disablesleep 1\n"
	if got != want {
		t.Fatalf("rule:\n%s\nwant:\n%s", got, want)
	}
	for _, bad := range []string{"", "root", "a b", "al,ice", "ALL", "ADMINS", "1234", "x\nALL ALL=(ALL) ALL", "-x", "%admin", "#501"} {
		if _, err := SudoersRule(bad); err == nil {
			t.Errorf("user %q accepted", bad)
		}
	}
}

func enableSteps(a Admin) [][]string {
	staged := filepath.Join(filepath.Dir(a.SudoersPath), ".togo-awake.tmp")
	return [][]string{
		{"/usr/bin/sudo", "/usr/bin/tee", staged},
		{"/usr/bin/sudo", "/usr/sbin/chown", "root:wheel", staged},
		{"/usr/bin/sudo", "/bin/chmod", "0440", staged},
		{"/usr/bin/sudo", "/usr/sbin/visudo", "-cf", staged},
		{"/usr/bin/sudo", "/bin/mv", "-f", staged, a.SudoersPath},
		{"/usr/bin/sudo", "-n", "/usr/bin/pmset", "-a", "disablesleep", "1"},
	}
}

func TestAdminEnableStagesChecksThenRenames(t *testing.T) {
	fr := &fakeRunner{}
	a := newAdmin(t, fr)
	if err := a.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := enableSteps(a)
	if !slices.EqualFunc(fr.argvs, want, slices.Equal) {
		t.Fatalf("ran\n%q\nwant\n%q", fr.argvs, want)
	}
	rule, _ := SudoersRule("alice")
	if got := fr.inputs[strings.Join(want[0], " ")]; got != rule {
		t.Errorf("tee got %q, want the rule", got)
	}
	if !overrideMarked(a.Home) {
		t.Fatal("disablesleep was set without the marker that lets the daemon clear it")
	}
}

func TestAdminEnableFailures(t *testing.T) {
	probe := newAdmin(t, &fakeRunner{})
	steps := enableSteps(probe)
	staged := steps[0][2]
	rmStaged := []string{"/usr/bin/sudo", "/bin/rm", "-f", staged}
	for i, step := range steps[:5] {
		t.Run(filepath.Base(step[1]), func(t *testing.T) {
			fr := &fakeRunner{fail: map[string]error{strings.Join(step, " "): errors.New("exit status 1")}}
			a := newAdmin(t, fr)
			a.SudoersPath = probe.SudoersPath
			if err := a.Enable(context.Background()); err == nil {
				t.Fatal("Enable succeeded")
			}
			want := append(slices.Clone(steps[:i+1]), rmStaged)
			if !slices.EqualFunc(fr.argvs, want, slices.Equal) {
				t.Fatalf("ran\n%q\nwant\n%q", fr.argvs, want)
			}
			if overrideMarked(a.Home) {
				t.Fatal("marker written although disablesleep was never set")
			}
		})
	}
	t.Run("pmset", func(t *testing.T) {
		fr := &fakeRunner{fail: map[string]error{strings.Join(steps[5], " "): errors.New("sudo: a password is required")}}
		a := newAdmin(t, fr)
		a.SudoersPath = probe.SudoersPath
		if err := a.Enable(context.Background()); err == nil {
			t.Fatal("Enable succeeded")
		}
		want := append(slices.Clone(steps),
			[]string{"/usr/bin/sudo", "/usr/bin/pmset", "-a", "disablesleep", "0"},
			[]string{"/usr/bin/sudo", "/bin/rm", "-f", a.SudoersPath})
		if !slices.EqualFunc(fr.argvs, want, slices.Equal) {
			t.Fatalf("ran\n%q\nwant\n%q", fr.argvs, want)
		}
		if overrideMarked(a.Home) {
			t.Fatal("marker left after disablesleep was restored")
		}
	})
}

func TestAdminRemove(t *testing.T) {
	fr := &fakeRunner{}
	a := newAdmin(t, fr)
	path := a.SudoersPath
	if a.Installed() {
		t.Fatal("installed before the file exists")
	}
	if removed, err := a.Remove(context.Background()); removed || err != nil || len(fr.argvs) != 0 {
		t.Fatalf("remove without a file: %v %v %q", removed, err, fr.argvs)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !a.Installed() {
		t.Fatal("not installed with the file present")
	}
	if removed, err := a.Remove(context.Background()); !removed || err != nil {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if want := []string{"/usr/bin/sudo", "/bin/rm", "-f", path}; len(fr.argvs) != 1 || !slices.Equal(fr.argvs[0], want) {
		t.Fatalf("ran %q, want %q", fr.argvs, want)
	}
}

func TestSudoPmsetNeverPrompts(t *testing.T) {
	fr := &fakeRunner{}
	o := SudoPmset{Run: fr.run}
	_ = o.Set(context.Background(), true)
	_ = o.Set(context.Background(), false)
	want := [][]string{
		{"/usr/bin/sudo", "-n", "/usr/bin/pmset", "-a", "disablesleep", "1"},
		{"/usr/bin/sudo", "-n", "/usr/bin/pmset", "-a", "disablesleep", "0"},
	}
	if !slices.EqualFunc(fr.argvs, want, slices.Equal) {
		t.Fatalf("ran %q, want %q", fr.argvs, want)
	}
}

func TestClearOfflineOnlyClearsWhatTogoSet(t *testing.T) {
	home := t.TempDir()
	o := &fakeOverride{}
	if cleared, err := ClearOverride(context.Background(), home, o); cleared || err != nil || len(o.calls) != 0 {
		t.Fatalf("no marker: %v %v %v", cleared, err, o.calls)
	}
	if err := markOverride(home); err != nil {
		t.Fatal(err)
	}
	if cleared, err := ClearOverride(context.Background(), home, o); !cleared || err != nil || !equalBools(o.calls, []bool{false}) {
		t.Fatalf("marker: %v %v %v", cleared, err, o.calls)
	}
	if overrideMarked(home) {
		t.Fatal("marker left behind")
	}
	o.err = errors.New("sudo: a password is required")
	_ = markOverride(home)
	if _, err := ClearOverride(context.Background(), home, o); err == nil || !overrideMarked(home) {
		t.Fatalf("failed clear dropped the marker: %v", err)
	}
}

func TestAdminRemoveClearsOverrideFirst(t *testing.T) {
	fr := &fakeRunner{}
	a := newAdmin(t, fr)
	if err := markOverride(a.Home); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.Remove(context.Background()); removed || err != nil {
		t.Fatalf("remove without a rule: %v %v", removed, err)
	}
	want := []string{"/usr/bin/sudo", "/usr/bin/pmset", "-a", "disablesleep", "0"}
	if len(fr.argvs) != 1 || !slices.Equal(fr.argvs[0], want) || overrideMarked(a.Home) {
		t.Fatalf("ran %q, marker %v", fr.argvs, overrideMarked(a.Home))
	}
}

func TestSudoersRuleAcceptsMixedCaseNames(t *testing.T) {
	for _, name := range []string{"Alice", "j.doe", "2fast", "dev_1"} {
		if _, err := SudoersRule(name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
}
