package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stub(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeRun answers by the base name of the binary.
func fakeRun(outputs map[string]string) func(context.Context, string, ...string) (string, error) {
	return func(_ context.Context, bin string, _ ...string) (string, error) {
		return outputs[filepath.Base(bin)], nil
	}
}

func byName(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", name, checks)
	return Check{}
}

func TestDoctorAllPresent(t *testing.T) {
	bin := t.TempDir()
	for _, n := range []string{"tmux", "mosh-server", "etserver"} {
		stub(t, bin, n)
	}
	checks := Run(context.Background(), Env{
		Path: bin, GOOS: "linux",
		Run: fakeRun(map[string]string{"tmux": "tmux 3.6a\n", "mosh-server": "mosh-server (mosh 1.4.0)\nmore\n"}),
	})
	for _, name := range []string{"tmux", "mosh-server", "etserver"} {
		if c := byName(t, checks, name); c.Status != StatusOK {
			t.Errorf("%s status = %q, want ok", name, c.Status)
		}
	}
	if c := byName(t, checks, "tmux"); c.Version != "3.6" || c.Path != filepath.Join(bin, "tmux") {
		t.Errorf("tmux = %+v", c)
	}
	if c := byName(t, checks, "mosh-server"); c.Version != "mosh-server (mosh 1.4.0)" {
		t.Errorf("mosh version = %q", c.Version)
	}
}

func TestDoctorMoshMissing(t *testing.T) {
	checks := Run(context.Background(), Env{Path: t.TempDir(), GOOS: "darwin", Run: fakeRun(nil)})
	c := byName(t, checks, "mosh-server")
	if c.Status != StatusMissing || c.Fix != "brew install mosh" {
		t.Fatalf("mosh-server = %+v", c)
	}
	if got := byName(t, checks, "tmux").Fix; got != "brew install tmux" {
		t.Errorf("tmux fix = %q", got)
	}
}

func TestDoctorMissingFixOnLinux(t *testing.T) {
	checks := Run(context.Background(), Env{Path: t.TempDir(), GOOS: "linux", Run: fakeRun(nil)})
	if got := byName(t, checks, "mosh-server").Fix; got != "sudo apt install mosh" {
		t.Errorf("mosh fix = %q", got)
	}
}

func TestDoctorMoshOffPath(t *testing.T) {
	fb := t.TempDir()
	want := stub(t, fb, "mosh-server")
	checks := Run(context.Background(), Env{
		Path: t.TempDir(), GOOS: "linux",
		Fallbacks: map[string][]string{"mosh-server": {fb}},
		Run:       fakeRun(map[string]string{"mosh-server": "mosh-server (mosh 1.4.0)"}),
	})
	c := byName(t, checks, "mosh-server")
	if c.Status != StatusOK || c.Path != want {
		t.Fatalf("mosh-server = %+v", c)
	}
	if !strings.HasSuffix(c.Detail, "found outside the login PATH; Togo finds it, other Mosh clients may not") {
		t.Errorf("detail = %q", c.Detail)
	}
}

func TestDoctorOldTmuxWarns(t *testing.T) {
	bin := t.TempDir()
	stub(t, bin, "tmux")
	checks := Run(context.Background(), Env{Path: bin, GOOS: "linux", Run: fakeRun(map[string]string{"tmux": "tmux 2.9"})})
	if c := byName(t, checks, "tmux"); c.Status != StatusWarn {
		t.Fatalf("tmux = %+v", c)
	}
}

func TestDoctorEtserverOptional(t *testing.T) {
	checks := Run(context.Background(), Env{Path: t.TempDir(), GOOS: "linux", Run: fakeRun(nil)})
	if c := byName(t, checks, "etserver"); c.Status != StatusNote {
		t.Fatalf("etserver = %+v", c)
	}
}

func TestDoctorFirewall(t *testing.T) {
	run := func(goos, out string) Check {
		checks := Run(context.Background(), Env{Path: t.TempDir(), GOOS: goos, Run: fakeRun(map[string]string{"socketfilterfw": out})})
		return byName(t, checks, "firewall")
	}
	if c := run("darwin", "Firewall is enabled. (State = 1)"); c.Status != StatusWarn {
		t.Errorf("enabled = %+v", c)
	}
	if c := run("darwin", "Firewall is disabled. (State = 0)"); c.Status != StatusOK {
		t.Errorf("disabled = %+v", c)
	}
	if c := run("linux", ""); c.Status != StatusNote {
		t.Errorf("linux = %+v", c)
	}
}

func TestDoctorTailscaleSSH(t *testing.T) {
	bin := t.TempDir()
	stub(t, bin, "tailscale")
	run := func(prefs string) []Check {
		return Run(context.Background(), Env{Path: bin, GOOS: "linux", Run: fakeRun(map[string]string{"tailscale": prefs})})
	}
	if c := byName(t, run(`{"RunSSH":true}`), "tailscale"); c.Status != StatusNote {
		t.Fatalf("tailscale = %+v", c)
	}
	for _, c := range run(`{"RunSSH":false}`) {
		if c.Name == "tailscale" {
			t.Fatal("tailscale check present with SSH off")
		}
	}
}

func TestDefaultFallbacksExpandHome(t *testing.T) {
	fb := DefaultFallbacks("/h")
	found := false
	for name, dirs := range fb {
		for _, d := range dirs {
			if strings.Contains(d, "$HOME") || strings.Contains(d, "~") {
				t.Errorf("%s has unexpanded dir %q", name, d)
			}
			if name == "mosh-server" && d == "/h/.nix-profile/bin" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("mosh-server fallbacks = %v, want /h/.nix-profile/bin", fb["mosh-server"])
	}
	if len(fb["tmux"]) == 0 {
		t.Error("no tmux fallbacks")
	}
}
