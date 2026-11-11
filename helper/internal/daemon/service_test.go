package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeRunner records every command and reports the job as loaded only after
// something loaded it.
type fakeRunner struct {
	calls  [][]string
	loaded bool
}

func (f *fakeRunner) run(_ context.Context, argv []string) error {
	f.calls = append(f.calls, slices.Clone(argv))
	switch {
	case slices.Contains(argv, "print"), slices.Contains(argv, "is-active"):
		if !f.loaded {
			return errors.New("not loaded")
		}
	case slices.Contains(argv, "bootstrap"), slices.Contains(argv, "restart"):
		f.loaded = true
	case slices.Contains(argv, "bootout"), slices.Contains(argv, "disable"):
		f.loaded = false
	}
	return nil
}

// mutating drops the read-only status queries.
func (f *fakeRunner) mutating() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if !slices.Contains(c, "print") && !slices.Contains(c, "is-active") {
			out = append(out, c)
		}
	}
	return out
}

func newTestService(t *testing.T, m Manager) (Service, *fakeRunner) {
	t.Helper()
	home := t.TempDir()
	r := &fakeRunner{}
	return Service{
		Manager:  m,
		Home:     home,
		TogoHome: filepath.Join(home, ".togo"),
		Exe:      "/opt/togo bin/togo",
		UID:      501,
		Run:      r.run,
	}, r
}

func checkFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s =\n%s\nwant\n%s", path, data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
}

func TestServiceInstallWritesPlist(t *testing.T) {
	s, r := newTestService(t, Launchd)
	if err := s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Home, "Library", "LaunchAgents", "dev.togoapp.togo.plist")
	logPath := filepath.Join(s.Home, ".togo", "log", "daemon.log")
	checkFile(t, path, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.togoapp.togo</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/togo bin/togo</string>
		<string>daemon</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>TOGO_HOME</key>
		<string>`+filepath.Join(s.Home, ".togo")+`</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>`+logPath+`</string>
	<key>StandardErrorPath</key>
	<string>`+logPath+`</string>
</dict>
</plist>
`)
	want := [][]string{{"launchctl", "bootstrap", "gui/501", path}}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestServiceInstallWritesSystemdUnit(t *testing.T) {
	s, r := newTestService(t, Systemd)
	s.Exe = `/opt/a"b%c$d/togo`
	if err := s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Home, ".config", "systemd", "user", "togo.service")
	checkFile(t, path, `[Unit]
Description=Togo helper daemon

[Service]
ExecStart="/opt/a\"b%%c$$d/togo" daemon
Environment="TOGO_HOME=`+filepath.Join(s.Home, ".togo")+`"
Restart=on-failure

[Install]
WantedBy=default.target
`)
	want := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "togo.service"},
		{"systemctl", "--user", "restart", "togo.service"},
	}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestServiceReinstallReloadsLaunchdJob(t *testing.T) {
	s, r := newTestService(t, Launchd)
	ctx := context.Background()
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(ctx); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	got := r.mutating()
	if len(got) != 3 || got[1][1] != "bootout" || got[2][1] != "bootstrap" {
		t.Errorf("commands = %q, want bootstrap, bootout, bootstrap", got)
	}
}

func TestServiceInstallRejectsRelativeBinary(t *testing.T) {
	s, r := newTestService(t, Launchd)
	s.Exe = "togo"
	if err := s.Install(context.Background()); err == nil {
		t.Fatal("Install accepted a relative binary path")
	}
	if len(r.calls) != 0 {
		t.Errorf("commands ran: %q", r.calls)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Errorf("service file written (stat: %v)", err)
	}
}

func TestServiceUninstallRemovesOnlyOurFile(t *testing.T) {
	for _, m := range []Manager{Launchd, Systemd} {
		s, r := newTestService(t, m)
		ctx := context.Background()
		if err := s.Install(ctx); err != nil {
			t.Fatal(err)
		}
		neighbour := filepath.Join(filepath.Dir(s.Path()), "com.example.other.plist")
		if err := os.WriteFile(neighbour, []byte("theirs"), 0o644); err != nil {
			t.Fatal(err)
		}
		r.calls = nil
		if err := s.Uninstall(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
			t.Errorf("manager %d: service file still present (stat: %v)", m, err)
		}
		if data, err := os.ReadFile(neighbour); err != nil || string(data) != "theirs" {
			t.Errorf("manager %d: neighbouring file = %q, %v; want it untouched", m, data, err)
		}
		entries, _ := os.ReadDir(filepath.Dir(s.Path()))
		if len(entries) != 1 {
			t.Errorf("manager %d: directory holds %d entries, want only the neighbour", m, len(entries))
		}
		want := [][]string{{"launchctl", "bootout", "gui/501/dev.togoapp.togo"}}
		if m == Systemd {
			want = [][]string{
				{"systemctl", "--user", "disable", "--now", "togo.service"},
				{"systemctl", "--user", "daemon-reload"},
			}
		}
		if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
			t.Errorf("manager %d: commands = %q, want %q", m, got, want)
		}
	}
}

func TestServiceUninstallWhenAbsentRunsNothing(t *testing.T) {
	s, r := newTestService(t, Launchd)
	if err := s.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Errorf("commands ran: %q", r.calls)
	}
}

func TestServiceStatusReportsFileAndJob(t *testing.T) {
	s, _ := newTestService(t, Launchd)
	ctx := context.Background()
	st, err := s.Status(ctx)
	if err != nil || st.Installed || st.Loaded {
		t.Fatalf("Status before install = %+v, %v; want neither", st, err)
	}
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	st, err = s.Status(ctx)
	if err != nil || !st.Installed || !st.Loaded || !strings.HasSuffix(st.Path, ".plist") {
		t.Fatalf("Status after install = %+v, %v; want installed and loaded", st, err)
	}
}

func TestSystemdUninstallDisablesInactiveUnit(t *testing.T) {
	s, r := newTestService(t, Systemd)
	ctx := context.Background()
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	r.loaded = false
	r.calls = nil
	if err := s.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"systemctl", "--user", "disable", "--now", "togo.service"},
		{"systemctl", "--user", "daemon-reload"},
	}
	if got := r.mutating(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestServiceStartUsesManager(t *testing.T) {
	for _, tc := range []struct {
		m    Manager
		want []string
	}{
		{Launchd, []string{"launchctl", "kickstart", "gui/501/" + Label}},
		{Systemd, []string{"systemctl", "--user", "start", systemdUnit}},
	} {
		s, r := newTestService(t, tc.m)
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 1 || !slices.Equal(r.calls[0], tc.want) {
			t.Errorf("calls = %v, want [%v]", r.calls, tc.want)
		}
	}
}

func TestServiceManaged(t *testing.T) {
	ctx := context.Background()
	s, r := newTestService(t, Launchd)
	if s.Managed(ctx) {
		t.Error("managed with nothing installed")
	}
	if err := s.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Managed(ctx) {
		t.Error("not managed after install")
	}
	other := s
	other.TogoHome = filepath.Join(s.Home, "other")
	if other.Managed(ctx) {
		t.Error("managed for a TOGO_HOME the service was not installed for")
	}
	r.loaded = false // booted out but the file remains
	if s.Managed(ctx) {
		t.Error("managed while not loaded")
	}
	r.loaded = true
	if err := s.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Managed(ctx) {
		t.Error("managed after uninstall")
	}
}
