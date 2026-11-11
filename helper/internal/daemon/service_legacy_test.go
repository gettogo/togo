package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const oldLabel = "io.github.tvlha.togo"

// labelRunner is a launchctl that tracks which labels are loaded, so the
// old and new jobs can be told apart.
type labelRunner struct {
	calls     [][]string
	loaded    map[string]bool
	bootoutOK bool
}

func (f *labelRunner) run(_ context.Context, argv []string) error {
	f.calls = append(f.calls, slices.Clone(argv))
	target := argv[len(argv)-1]
	switch argv[1] {
	case "print":
		if !f.loaded[target] {
			return errors.New("not loaded")
		}
	case "bootout":
		if !f.bootoutOK {
			return errors.New("bootout failed")
		}
		delete(f.loaded, target)
	case "bootstrap":
		f.loaded["gui/501/"+Label] = true
	}
	return nil
}

func (f *labelRunner) mutating() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[1] != "print" {
			out = append(out, c)
		}
	}
	return out
}

// legacyService is a Mac running the daemon under the old label.
func legacyService(t *testing.T, loaded bool) (Service, *labelRunner, string) {
	t.Helper()
	s, _ := newTestService(t, Launchd)
	r := &labelRunner{loaded: map[string]bool{}, bootoutOK: true}
	s.Run = r.run
	old := filepath.Join(s.Home, "Library", "LaunchAgents", oldLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loaded {
		r.loaded["gui/501/"+oldLabel] = true
	}
	return s, r, old
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestInstallRetiresLegacyLaunchAgent(t *testing.T) {
	s, r, old := legacyService(t, true)
	neighbour := filepath.Join(filepath.Dir(old), "com.example.other.plist")
	if err := os.WriteFile(neighbour, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"launchctl", "bootout", "gui/501/" + oldLabel},
		{"launchctl", "bootstrap", "gui/501", s.Path()},
	}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
	if exists(old) {
		t.Error("old plist still present")
	}
	if !exists(s.Path()) || filepath.Base(s.Path()) != "dev.togoapp.togo.plist" {
		t.Errorf("new plist missing at %s", s.Path())
	}
	if data, err := os.ReadFile(neighbour); err != nil || string(data) != "theirs" {
		t.Errorf("neighbouring file = %q, %v; want it untouched", data, err)
	}
	if r.loaded["gui/501/"+oldLabel] || !r.loaded["gui/501/"+Label] {
		t.Errorf("loaded = %v, want only the new label", r.loaded)
	}

	// A second install is an ordinary reinstall: the old label is gone.
	r.calls = nil
	if err := s.Install(ctx); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	want = [][]string{
		{"launchctl", "bootout", "gui/501/" + Label},
		{"launchctl", "bootstrap", "gui/501", s.Path()},
	}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("second install commands = %q, want %q", got, want)
	}
}

func TestInstallRemovesUnloadedLegacyPlist(t *testing.T) {
	s, r, old := legacyService(t, false)
	if err := s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"launchctl", "bootstrap", "gui/501", s.Path()}}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
	if exists(old) {
		t.Error("old plist still present")
	}
}

func TestInstallKeepsLegacyWhenBootoutFails(t *testing.T) {
	s, r, old := legacyService(t, true)
	r.bootoutOK = false
	ctx := context.Background()
	if err := s.Install(ctx); err == nil {
		t.Fatal("Install succeeded with the old job still running")
	}
	if !exists(old) {
		t.Error("old plist removed while its job still runs")
	}
	if exists(s.Path()) {
		t.Error("new plist written while the old job still runs")
	}
	for _, c := range r.calls {
		if c[1] == "bootstrap" {
			t.Errorf("bootstrapped with the old job still running: %q", c)
		}
	}

	// Once launchctl cooperates, running install again finishes the move.
	r.bootoutOK = true
	if err := s.Install(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if exists(old) || !exists(s.Path()) {
		t.Errorf("after retry: old present %v, new present %v", exists(old), exists(s.Path()))
	}
}

func TestUninstallRemovesLegacyLaunchAgent(t *testing.T) {
	s, r, old := legacyService(t, true)
	if !s.HasFile() {
		t.Fatal("HasFile = false with an old plist on disk")
	}
	if err := s.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"launchctl", "bootout", "gui/501/" + oldLabel}}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
	if exists(old) || s.HasFile() {
		t.Error("a service file is left behind")
	}
}

func TestStatusReportsLegacyLaunchAgent(t *testing.T) {
	s, _, old := legacyService(t, true)
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Status{Path: old, Installed: true, Loaded: true, Legacy: true}); st != want {
		t.Errorf("Status = %+v, want %+v", st, want)
	}
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	st, err = s.Status(ctx)
	if err != nil || st.Legacy || st.Path != s.Path() || !st.Installed || !st.Loaded {
		t.Errorf("Status after install = %+v, %v; want the new service", st, err)
	}
}
