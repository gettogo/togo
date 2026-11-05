package main

import (
	"bytes"
	"context"
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

func TestUpdateRefusesHomebrewInstall(t *testing.T) {
	shortHome(t)
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", releasedBinary)
	exe := filepath.Join(t.TempDir(), "Cellar", "togo", "1.0.0", "bin", "togo")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, installedBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	setUpdate(t, s.Source(), exe)
	setVersion(t, "v1.0.0")
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"update"}, nil, nil, &out, &errOut)
	if code != exitError || !strings.Contains(errOut.String(), "Installed with Homebrew: run brew upgrade togo") {
		t.Fatalf("code=%d stderr=%q, want the brew instruction", code, errOut.String())
	}
	binaryIs(t, exe, installedBinary)
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
