package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
	"github.com/gettogo/togo/helper/internal/update"
	"github.com/gettogo/togo/helper/internal/update/updatetest"
)

var (
	installedBinary = []byte("#!/bin/sh\necho installed\n")
	releasedBinary  = []byte("#!/bin/sh\necho released\n")
)

// useRelease serves releases from s, makes update replace a temporary
// binary, and returns that binary's path.
func useRelease(t *testing.T, s *updatetest.Server) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "togo")
	if err := os.WriteFile(exe, installedBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	setUpdate(t, s.Source(), exe)
	return exe
}

func setUpdate(t *testing.T, src update.Source, exe string) {
	t.Helper()
	oldSource, oldExe := updateSource, updateExecutable
	updateSource = func() (update.Source, error) { return src, nil }
	updateExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { updateSource, updateExecutable = oldSource, oldExe })
}

func binaryIs(t *testing.T, exe string, want []byte) {
	t.Helper()
	if got, err := os.ReadFile(exe); err != nil || !bytes.Equal(got, want) {
		t.Errorf("binary = %q, %v; want %q", got, err, want)
	}
}

// useHomebrew points update at a fake Cellar install with a brew stub,
// and replaces the brew runner and version probe. It returns the calls
// brew received.
type brewCall struct {
	brew      string
	args, env []string
}

func useHomebrew(t *testing.T, s *updatetest.Server, installedAfter string, brewErr error, brewStderr string) *[]brewCall {
	t.Helper()
	prefix := t.TempDir()
	exe := filepath.Join(prefix, "Cellar", "togo", "1.0.0", "bin", "togo")
	for _, p := range []string{exe, filepath.Join(prefix, "bin", "brew")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, installedBinary, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	setUpdate(t, s.Source(), exe)
	var calls []brewCall
	oldRun, oldVer := brewRun, brewInstalledVersion
	brewRun = func(_ context.Context, brew string, args, env []string) (string, error) {
		calls = append(calls, brewCall{brew, args, env})
		return brewStderr, brewErr
	}
	brewInstalledVersion = func(context.Context, string) (string, error) { return installedAfter, nil }
	t.Cleanup(func() { brewRun, brewInstalledVersion = oldRun, oldVer })
	return &calls
}

func TestUpdateCheckWorksForHomebrewInstall(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	calls := useHomebrew(t, s, "v1.0.1", nil, "")
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"update", "--check"}, nil, nil, &out, &errOut); code != exitOK || out.String() != "togo v1.0.1 is available (installed: v1.0.0)\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	setVersion(t, "v1.0.1")
	out.Reset()
	if code := runContext(context.Background(), []string{"update", "--check"}, nil, nil, &out, &errOut); code != exitOK || out.String() != "togo v1.0.1 is up to date\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if len(*calls) != 0 {
		t.Errorf("--check ran brew: %v", *calls)
	}
}

func TestUpdateRunsHomebrewUpgrade(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", t.TempDir())
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	calls := useHomebrew(t, s, "v1.0.1", nil, "")
	setVersion(t, "v1.0.0")
	old := daemontest.StartFake(t, home, "v1.0.0", nil)
	var restarted atomic.Bool
	oldStart := startDaemon
	startDaemon = func(h string) error {
		daemontest.StartFake(t, h, "v1.0.1", nil)
		restarted.Store(true)
		return nil
	}
	t.Cleanup(func() { startDaemon = oldStart })

	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
	if code != exitOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("brew calls = %v, want one", *calls)
	}
	c := (*calls)[0]
	if !strings.HasSuffix(c.brew, filepath.Join("bin", "brew")) || strings.Join(c.args, " ") != "upgrade gettogo/tap/togo" {
		t.Errorf("brew = %q %v", c.brew, c.args)
	}
	for _, want := range []string{"NONINTERACTIVE=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"} {
		found := false
		for _, e := range c.env {
			found = found || e == want
		}
		if !found {
			t.Errorf("env %v lacks %s", c.env, want)
		}
	}
	if !strings.Contains(out.String(), "Installed togo v1.0.1 with Homebrew\n") || !strings.HasSuffix(out.String(), "Restarted\n") {
		t.Errorf("stdout = %q", out.String())
	}
	waitDone(t, old)
	if !restarted.Load() {
		t.Error("no daemon started after the old one exited")
	}
}

func TestUpdateHomebrewUpToDateSkipsBrew(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	calls := useHomebrew(t, s, "v1.0.1", nil, "")
	setVersion(t, "v1.0.1")
	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut); code != exitOK || out.String() != "togo v1.0.1 is up to date\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if len(*calls) != 0 {
		t.Errorf("brew ran: %v", *calls)
	}
}

func TestUpdateHomebrewFailureShowsBrewError(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	useHomebrew(t, s, "v1.0.1", errors.New("exit status 1"), "Error: disk full\n")
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
	if code != exitError || !strings.Contains(errOut.String(), "Error: disk full") || strings.Contains(out.String(), "Installed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestUpdateHomebrewLaggingFormula(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	useHomebrew(t, s, "v1.0.0", nil, "")
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
	if code != exitError || !strings.Contains(errOut.String(), "Homebrew doesn't have togo v1.0.1 yet. Try again later.") || strings.Contains(out.String(), "Installed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestUpdateHomebrewRejectsVersionFlag(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	calls := useHomebrew(t, s, "v1.0.1", nil, "")
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"update", "--version", "v1.0.1"}, nil, nil, &out, &errOut)
	if code != exitError || !strings.Contains(errOut.String(), "not supported for a Homebrew install") || len(*calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%v", code, errOut.String(), *calls)
	}
}

func TestUpdateRefusesOlderVersion(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.0", releasedBinary)
	exe := useRelease(t, s)
	setVersion(t, "v1.0.1")

	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut); code != exitOK || out.String() != "togo v1.0.1 is up to date\n" {
		t.Fatalf("update: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	binaryIs(t, exe, installedBinary)

	out.Reset()
	errOut.Reset()
	if code := runContext(context.Background(), []string{"update", "--version", "v1.0.0"}, nil, nil, &out, &errOut); code != exitError || !strings.Contains(errOut.String(), "--allow-downgrade") {
		t.Fatalf("update --version older: code=%d stderr=%q, want a refusal naming --allow-downgrade", code, errOut.String())
	}
	binaryIs(t, exe, installedBinary)

	out.Reset()
	errOut.Reset()
	if code := runContext(context.Background(), []string{"update", "--version", "v1.0.0", "--allow-downgrade"}, nil, nil, &out, &errOut); code != exitOK {
		t.Fatalf("update --allow-downgrade: code=%d stderr=%q", code, errOut.String())
	}
	binaryIs(t, exe, releasedBinary)
}

func TestUpdateCheckReportsNewerRelease(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	exe := useRelease(t, s)
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"update", "--check"}, nil, nil, &out, &errOut); code != exitOK || out.String() != "togo v1.0.1 is available (installed: v1.0.0)\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	binaryIs(t, exe, installedBinary)
}

func TestUpdateRestartsDaemonWhenIdle(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		home := shortHome(t)
		t.Setenv("HOME", t.TempDir())
		s := updatetest.NewServer(t)
		s.Publish(t, "v1.0.1", releasedBinary)
		exe := useRelease(t, s)
		setVersion(t, "v1.0.0")
		old := daemontest.StartFake(t, home, "v1.0.0", nil)
		var restarted atomic.Bool
		oldStart := startDaemon
		startDaemon = func(h string) error {
			daemontest.StartFake(t, h, "v1.0.1", nil)
			restarted.Store(true)
			return nil
		}
		t.Cleanup(func() { startDaemon = oldStart })

		var out, errOut bytes.Buffer
		code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
		if code != exitOK {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		binaryIs(t, exe, releasedBinary)
		if !strings.Contains(out.String(), "Installed togo v1.0.1") || !strings.HasSuffix(out.String(), "Restarted\n") {
			t.Errorf("stdout = %q, want the install and the restart", out.String())
		}
		waitDone(t, old)
		if !restarted.Load() {
			t.Error("no daemon started after the old one exited")
		}
	})
	t.Run("approval pending", func(t *testing.T) {
		home := shortHome(t)
		t.Setenv("HOME", t.TempDir())
		s := updatetest.NewServer(t)
		s.Publish(t, "v1.0.1", releasedBinary)
		exe := useRelease(t, s)
		setVersion(t, "v1.0.0")
		var open atomic.Int32
		open.Store(1)
		old := daemontest.StartFake(t, home, "v1.0.0", func() int { return int(open.Load()) })

		var out, errOut bytes.Buffer
		code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
		if code != exitOK || !strings.HasSuffix(out.String(), "Restart waits for 1 pending approval\n") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		binaryIs(t, exe, releasedBinary)
		select {
		case err := <-old.Done:
			t.Fatalf("daemon stopped (%v) with an approval pending", err)
		default:
		}
		open.Store(0)
		waitDone(t, old)
	})
}

func TestUpdateRefusesTamperedArchive(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	name := update.ArchiveName("v1.0.1", runtime.GOOS, runtime.GOARCH)
	s.Put("v1.0.1", name, updatetest.Archive(t, []byte("evil")))
	exe := useRelease(t, s)
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	if code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut); code != exitError {
		t.Fatalf("code=%d stdout=%q, want a failure", code, out.String())
	}
	binaryIs(t, exe, installedBinary)
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("install directory holds %d entries, want only togo", len(entries))
	}
}
