package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/mailbox"
)

// stopEnv points HOME and TOGO_HOME at temporary directories and records
// whether the stop marker existed when the service manager was asked to
// stop the daemon.
func stopEnv(t *testing.T) (togoHome string, sawMarker *bool) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	togoHome = t.TempDir()
	t.Setenv("TOGO_HOME", togoHome)
	oldRunner, oldGOOS := serviceRunner, awakeGOOS
	t.Cleanup(func() { serviceRunner, awakeGOOS = oldRunner, oldGOOS })
	awakeGOOS = "plan9" // no sudoers rule to remove
	saw := false
	serviceRunner = func(context.Context, []string) error {
		if _, err := os.Stat(filepath.Join(togoHome, mailbox.StopMarkerName)); err == nil {
			saw = true
		}
		return nil
	}
	// An installed service, so uninstall has a daemon to stop.
	svc, err := userService()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(svc.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.Path(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return togoHome, &saw
}

func checkMarker(t *testing.T, togoHome string, saw bool) {
	t.Helper()
	if !saw {
		t.Fatal("service manager ran before the stop marker was written")
	}
	fi, err := os.Stat(filepath.Join(togoHome, mailbox.StopMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestServiceStopWritesMarker(t *testing.T) {
	home, saw := stopEnv(t)
	var out, errOut bytes.Buffer
	if code := runService(context.Background(), []string{"uninstall"}, &out, &errOut); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	checkMarker(t, home, *saw)
}

func TestUninstallWritesMarker(t *testing.T) {
	home, saw := stopEnv(t)
	var out, errOut bytes.Buffer
	runUninstall(context.Background(), nil, map[string]string{}, &out, &errOut)
	checkMarker(t, home, *saw)
}

func TestServiceStatusLeavesNoMarker(t *testing.T) {
	home, _ := stopEnv(t)
	var out, errOut bytes.Buffer
	runService(context.Background(), []string{"status"}, &out, &errOut)
	if _, err := os.Stat(filepath.Join(home, mailbox.StopMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("status wrote the marker: %v", err)
	}
}

func TestFailedUninstallLeavesNoMarker(t *testing.T) {
	home, _ := stopEnv(t)
	serviceRunner = func(_ context.Context, argv []string) error {
		if slices.Contains(argv, "bootout") || slices.Contains(argv, "disable") {
			return errors.New("stop failed")
		}
		return nil
	}
	var out, errOut bytes.Buffer
	if code := runService(context.Background(), []string{"uninstall"}, &out, &errOut); code == exitOK {
		t.Fatal("uninstall reported success")
	}
	if _, err := os.Stat(filepath.Join(home, mailbox.StopMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("marker left after a failed uninstall: %v", err)
	}
}

func TestUninstallWithoutServiceLeavesNoMarker(t *testing.T) {
	home, _ := stopEnv(t)
	svc, err := userService()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(svc.Path()); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runService(context.Background(), []string{"uninstall"}, &out, &errOut); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, mailbox.StopMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("marker written with no service to stop: %v", err)
	}
}
