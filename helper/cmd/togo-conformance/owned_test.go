package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setup snapshots one watched file of a fake harness in a temp folder.
func ownedSetup(t *testing.T) (string, []snapEntry) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(`{"model":"a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	watched["fake"] = []struct {
		path string
		hot  bool
	}{{p, false}}
	t.Cleanup(func() { delete(watched, "fake"); owned = &ledger{tracks: map[string]*track{}} })
	owned = &ledger{tracks: map[string]*track{}}
	snap, err := snapshot(filepath.Join(dir, "snap"), []string{"fake"})
	if err != nil {
		t.Fatal(err)
	}
	owned.start(filepath.Join(dir, "snap"), snap)
	return p, snap
}

func TestRestoreSuiteChange(t *testing.T) {
	p, snap := ownedSetup(t)
	owned.observe("fake", false) // run starts
	owned.observe("fake", true)  // first call
	_ = os.WriteFile(p, []byte(`{"model":"b"}`), 0o600)
	owned.observe("fake", true)  // next call sees the suite's change
	owned.observe("fake", false) // run ends
	notes := strings.Join(restore(snap, "tcrx"), "\n")
	if b, _ := os.ReadFile(p); string(b) != `{"model":"a"}` {
		t.Fatalf("not restored: %s\n%s", b, notes)
	}
	bk, _ := filepath.Glob(p + ".togo-conformance-backup-*")
	if len(bk) != 1 {
		t.Fatalf("backups %v", bk)
	}
	if b, _ := os.ReadFile(bk[0]); string(b) != `{"model":"b"}` {
		t.Errorf("backup holds %s", b)
	}
}

func TestRestoreLeavesOutsideChange(t *testing.T) {
	for name, step := range map[string]func(p string){
		"before first call": func(p string) {
			_ = os.WriteFile(p, []byte(`{"login":1}`), 0o600)
			owned.observe("fake", false)
			owned.observe("fake", true)
			owned.observe("fake", false)
		},
		"after run ended": func(p string) {
			owned.observe("fake", false)
			owned.observe("fake", true)
			owned.observe("fake", false)
			_ = os.WriteFile(p, []byte(`{"login":1}`), 0o600)
		},
		"suite then owner": func(p string) {
			owned.observe("fake", true)
			_ = os.WriteFile(p, []byte(`{"model":"b"}`), 0o600)
			owned.observe("fake", false)
			_ = os.WriteFile(p, []byte(`{"model":"b","login":1}`), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, snap := ownedSetup(t)
			step(p)
			want, _ := os.ReadFile(p)
			notes := strings.Join(restore(snap, "tcrx"), "\n")
			if b, _ := os.ReadFile(p); string(b) != string(want) {
				t.Fatalf("restored over an outside change: %s", b)
			}
			if !strings.Contains(notes, "left ") || !strings.Contains(notes, "changed outside the suite") {
				t.Errorf("notes = %q", notes)
			}
			if bk, _ := filepath.Glob(p + ".togo-conformance-backup-*"); len(bk) != 0 {
				t.Errorf("backup made without a restore: %v", bk)
			}
		})
	}
}

func TestRestoreWithoutLedgerLeavesFile(t *testing.T) {
	p, snap := ownedSetup(t)
	_ = os.WriteFile(p, []byte(`{"model":"b"}`), 0o600)
	owned = &ledger{tracks: map[string]*track{}} // -cleanup with no ledger on disk
	restore(snap, "tcrx")
	if b, _ := os.ReadFile(p); string(b) != `{"model":"b"}` {
		t.Fatalf("restored with no record: %s", b)
	}
}
