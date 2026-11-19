// Package testtmp gathers the temporary folders of one test run under one
// folder, so a run that is killed or times out leaves one folder behind
// and not thousands, and the next run removes it.
//
// A test binary's TestMain calls Main:
//
//	func TestMain(m *testing.M) { os.Exit(testtmp.Main(m)) }
package testtmp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const prefix = "togo-run-"

// Main runs the tests with TMPDIR pointing at a folder of this run's own,
// and removes the folder afterwards. Folders t.TempDir made that could not
// be removed (something still held a file in them) are reported, then
// removed. Before the run it removes the folders of runs whose process is
// gone. TOGO_TEST_NO_SWEEP=1 keeps those.
func Main(m *testing.M) int {
	base := os.TempDir()
	if os.Getenv("TOGO_TEST_NO_SWEEP") == "" {
		sweep(base)
	}
	root, err := os.MkdirTemp(base, prefix+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return m.Run()
	}
	old, had := os.LookupEnv("TMPDIR")
	_ = os.Setenv("TMPDIR", root)
	code := m.Run()
	if had {
		_ = os.Setenv("TMPDIR", old)
	} else {
		_ = os.Unsetenv("TMPDIR")
	}
	if left := leftovers(root); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "testtmp: %d folders of this run could not be removed by their tests:\n  %s\n", len(left), strings.Join(left, "\n  "))
	}
	_ = os.RemoveAll(root)
	return code
}

// leftovers lists the folders under root that remain.
func leftovers(root string) []string {
	ents, _ := os.ReadDir(root)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// sweep removes the run folders in dir whose process is gone.
func sweep(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		pid, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(name, prefix), "-", 2)[0])
		if err != nil || running(pid) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, name))
	}
}

func running(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
