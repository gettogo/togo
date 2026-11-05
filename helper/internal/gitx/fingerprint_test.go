package gitx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestFingerprintBudget(t *testing.T) {
	defer gitx.SetFingerprintBudget(10)()
	dir := seeded(t)
	s := writer(dir)
	for _, n := range []string{"f1", "f2", "f3"} {
		write(t, dir, n, "abc\n")
	}
	write(t, dir, "f4", "\n")
	got := entries(statusOf(t, s))
	for n, blob := range map[string]bool{"f1": true, "f2": true, "f3": false, "f4": false} {
		if fp := got[n].Fingerprint; strings.HasPrefix(fp, "blob:") != blob || fp == "" {
			t.Errorf("%s: fingerprint %q, want blob %v", n, fp, blob)
		}
	}
	// A write walks the same list, so a stat fingerprint the client saw
	// matches; a write of fewer paths does not shift the budget.
	if _, err := writeCall(t, s, "git.stage", []string{"f4"}, seenAll(t, s, "f4")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCall(t, s, "git.stage", []string{"f2", "f3"}, seenAll(t, s, "f2", "f3")); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintSkipsUnreadableWithoutRehashing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	dir := seeded(t)
	for _, n := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		write(t, dir, n, n+"\n")
	}
	if err := os.Chmod(filepath.Join(dir, "b.txt"), 0); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "count")
	s := &gitx.Service{Sessions: writeSessions{cwd: dir},
		Git: wrappedGit(t, "hash-object", "echo x >> '"+counter+"'")}
	got := entries(statusOf(t, s))
	for _, n := range []string{"a.txt", "c.txt", "d.txt"} {
		if !strings.HasPrefix(got[n].Fingerprint, "blob:") {
			t.Errorf("%s: %q", n, got[n].Fingerprint)
		}
	}
	if fp := got["b.txt"].Fingerprint; fp != "" {
		t.Errorf("unreadable file has fingerprint %q", fp)
	}
	if runs := strings.Count(read(t, counter), "x"); runs != 2 {
		t.Errorf("hash-object ran %d times, want 2", runs)
	}
	// A file the helper could not fingerprint is stale against any it saw.
	seen := map[string]wire.GitSeen{"b.txt": {Status: "??", Fingerprint: "blob:" + strings.Repeat("0", 40)}}
	if _, err := writeCall(t, s, "git.discard", []string{"b.txt"}, seen); conflictReason(err) != wire.ConflictStale {
		t.Errorf("got %v", err)
	}
}
