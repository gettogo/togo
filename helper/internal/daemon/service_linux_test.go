//go:build linux

package daemon

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeLinger struct {
	on         bool
	enableErr  error
	enableUser []string
}

func (f *fakeLinger) Enabled(string) (bool, error) { return f.on, nil }
func (f *fakeLinger) Enable(u string) error {
	f.enableUser = append(f.enableUser, u)
	return f.enableErr
}

func installWithLinger(t *testing.T, f *fakeLinger) string {
	t.Helper()
	var out bytes.Buffer
	oldC, oldN, oldU := lingerControl, lingerNotice, currentUser
	t.Cleanup(func() { lingerControl, lingerNotice, currentUser = oldC, oldN, oldU })
	lingerControl, lingerNotice = f, &out
	currentUser = func() (string, error) { return "ada", nil }
	s, _ := newTestService(t, Systemd)
	if err := s.Install(context.Background()); err != nil {
		t.Fatalf("install must succeed: %v", err)
	}
	return out.String()
}

func TestInstallEnablesLinger(t *testing.T) {
	f := &fakeLinger{}
	if out := installWithLinger(t, f); out != "" {
		t.Errorf("unexpected output %q", out)
	}
	if !slices.Equal(f.enableUser, []string{"ada"}) {
		t.Errorf("Enable calls = %q", f.enableUser)
	}
}

func TestInstallLingerFailurePrintsCommand(t *testing.T) {
	f := &fakeLinger{enableErr: errors.New("denied")}
	want := "Togo stops when you log out. To keep it running, run: sudo loginctl enable-linger ada\n"
	if out := installWithLinger(t, f); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestInstallLingerAlreadyOn(t *testing.T) {
	f := &fakeLinger{on: true}
	installWithLinger(t, f)
	if len(f.enableUser) != 0 {
		t.Errorf("Enable called: %q", f.enableUser)
	}
}

func TestBadUserNameRefused(t *testing.T) {
	var calls [][]string
	l := systemLinger{run: func(argv []string) (string, error) {
		calls = append(calls, argv)
		return "yes\n", nil
	}}
	for _, name := range []string{"", "Ada", "-rf", "a b", "a;b", "$(id)", "../x", "a\n"} {
		if _, err := l.Enabled(name); err == nil {
			t.Errorf("Enabled(%q) accepted", name)
		}
		if err := l.Enable(name); err == nil {
			t.Errorf("Enable(%q) accepted", name)
		}
	}
	if len(calls) != 0 {
		t.Errorf("loginctl ran for a bad name: %q", calls)
	}
	on, err := l.Enabled("ada")
	if err != nil || !on {
		t.Fatalf("Enabled(ada) = %v, %v", on, err)
	}
	want := []string{"/usr/bin/loginctl", "show-user", "ada", "-p", "Linger", "--value"}
	if !slices.Equal(calls[0], want) {
		t.Errorf("argv = %q", calls[0])
	}
}
