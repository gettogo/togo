package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLogin struct {
	linger bool
	kill   bool
	cgroup string
	cgErr  error
}

func (f fakeLogin) Linger(string) (bool, error)      { return f.linger, nil }
func (f fakeLogin) KillUserProcesses() (bool, error) { return f.kill, nil }
func (f fakeLogin) TmuxCgroup() (string, error)      { return f.cgroup, f.cgErr }

func lingerEnv(l fakeLogin) Env { return Env{GOOS: "linux", User: "ada", Login: l} }

func TestLingerCheckOff(t *testing.T) {
	c := LingerCheck(lingerEnv(fakeLogin{}))
	want := "Togo stops when you log out. To keep it running, run: sudo loginctl enable-linger ada"
	if c.Name != "linger" || c.Status != StatusWarn || c.Detail != "off" || c.Fix != want {
		t.Errorf("got %+v", c)
	}
}

func TestLingerCheckOn(t *testing.T) {
	c := LingerCheck(lingerEnv(fakeLogin{linger: true}))
	if c.Status != StatusOK || c.Detail != "on" {
		t.Errorf("got %+v", c)
	}
}

func TestLingerCheckKillUserProcessesInSessionScope(t *testing.T) {
	c := LingerCheck(lingerEnv(fakeLogin{linger: true, kill: true, cgroup: "0::/user.slice/user-1000.slice/session-3.scope\n"}))
	if c.Status != StatusWarn || !strings.HasPrefix(c.Fix, "Sessions started from a login shell stop when you log out.") {
		t.Errorf("got %+v", c)
	}
}

func TestLingerCheckServiceScopeOK(t *testing.T) {
	cg := "0::/user.slice/user-1000.slice/user@1000.service/app.slice/togo.service\n"
	if c := LingerCheck(lingerEnv(fakeLogin{linger: true, kill: true, cgroup: cg})); c.Status != StatusOK {
		t.Errorf("got %+v", c)
	}
	if c := LingerCheck(lingerEnv(fakeLogin{linger: true, kill: true, cgErr: errors.New("no server")})); c.Status != StatusOK {
		t.Errorf("no tmux server: got %+v", c)
	}
}

func TestRunIncludesLingerOnlyOnLinux(t *testing.T) {
	e := lingerEnv(fakeLogin{linger: true})
	e.Run = fakeRun(nil)
	byName(t, Run(t.Context(), e), "linger")
	e.GOOS = "darwin"
	for _, c := range Run(t.Context(), e) {
		if c.Name == "linger" {
			t.Fatal("linger line on macOS")
		}
	}
}

func TestKillUserProcessesPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"no files", nil, false},
		{"vendor yes", map[string]string{"usr/lib/systemd/logind.conf": "[Login]\nKillUserProcesses=yes\n"}, true},
		{"admin overrides vendor", map[string]string{
			"usr/lib/systemd/logind.conf": "[Login]\nKillUserProcesses=yes\n",
			"etc/systemd/logind.conf":     "[Login]\nKillUserProcesses=no\n"}, false},
		{"drop-in overrides admin", map[string]string{
			"etc/systemd/logind.conf":              "[Login]\nKillUserProcesses=no\n",
			"usr/lib/systemd/logind.conf.d/a.conf": "[Login]\nKillUserProcesses=yes"}, true},
		{"etc masks same name in run", map[string]string{
			"run/systemd/logind.conf.d/a.conf": "[Login]\nKillUserProcesses=yes\n",
			"etc/systemd/logind.conf.d/a.conf": "[Login]\nKillUserProcesses=no\n"}, false},
		{"lexical order, later wins", map[string]string{
			"etc/systemd/logind.conf.d/10-a.conf": "[Login]\nKillUserProcesses=yes\n",
			"run/systemd/logind.conf.d/20-b.conf": "[Login]\nKillUserProcesses=no\n"}, false},
		{"other section ignored", map[string]string{
			"etc/systemd/logind.conf": "[Other]\nKillUserProcesses=yes\n"}, false},
		{"commented out", map[string]string{
			"etc/systemd/logind.conf": "[Login]\n#KillUserProcesses=yes\n ; KillUserProcesses=yes\n"}, false},
		{"spaces and case", map[string]string{
			"etc/systemd/logind.conf": "  [Login]\n  KillUserProcesses = YES  \n"}, true},
		{"malformed lines", map[string]string{
			"etc/systemd/logind.conf": "[Login\nKillUserProcesses\n=yes\n[Login]\nKillUserProcesses\nKillUserProcesses=true"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for p, c := range tc.files {
				full := filepath.Join(root, p)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := killUserProcesses(root); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
