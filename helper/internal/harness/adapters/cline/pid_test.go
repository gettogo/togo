package cline

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// fakeProcs is a fixed process tree; Cline needs no open-file lookup.
type fakeProcs struct{ tree []int }

func (f fakeProcs) Tree(context.Context) []int { return f.tree }
func (fakeProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}

func (fakeProcs) Others(context.Context, []string, string) (bool, error) { return false, nil }

var _ harness.Procs = fakeProcs{}

// addSession writes a second session of the same folder, started later,
// whose manifest records pid (0 leaves it out).
func addSession(t *testing.T, h, id string, pid int, doc []byte) []byte {
	t.Helper()
	dir := filepath.Join(h, ".cline", "data", "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	man, _ := os.ReadFile("testdata/session.json")
	s := strings.ReplaceAll(string(man), fixtureID, id)
	s = strings.Replace(s, "2026-11-08T03:37:52.654Z", "2026-11-08T05:00:00.000Z", 1)
	if pid > 0 {
		s = strings.Replace(s, `"version": 1,`, `"version": 1, "pid": `+strconv.Itoa(pid)+`,`, 1)
	}
	_ = os.WriteFile(filepath.Join(dir, id+".json"), []byte(s), 0o600)
	d := strings.ReplaceAll(string(doc), fixtureID, id)
	d = strings.Replace(d, "2026-11-08T03:38:03.002Z", "2026-11-08T05:01:00.000Z", 1)
	return []byte(d)
}

// A Cline the user started in a terminal records its own pid in its
// manifest. A session whose process is not in this session's tree is never
// followed; one whose process is, is.
func TestMessagesFollowsOnlyItsOwnProcess(t *testing.T) {
	h, doc := home(t)
	foreignDoc := addSession(t, h, "1794200000000_foreign", 999, doc)
	ownDoc := addSession(t, h, "1794200000001_mine0", 101, doc)
	e := env(h, "2026-11-08T03:37:52Z")
	e.Procs = fakeProcs{tree: []int{100, 101}}
	m := newMessages(e)
	if items, _ := m.Map(harness.Record{Data: foreignDoc}); len(items) != 0 || m.State()["harness_session"] != "" {
		t.Fatalf("followed a foreign process's session: %v", m.State())
	}
	if items, _ := m.Map(harness.Record{Data: ownDoc}); len(items) == 0 || m.State()["harness_session"] != "1794200000001_mine0" {
		t.Fatalf("own session not followed: %v", m.State())
	}
}

// Once a session is followed, a newer session with no process evidence
// does not take over.
func TestMessagesKeepsSessionWithoutEvidence(t *testing.T) {
	h, doc := home(t)
	other := addSession(t, h, "1794200000002_nopid", 0, doc)
	e := env(h, "2026-11-08T03:37:52Z")
	e.Procs = fakeProcs{tree: []int{100}}
	m := newMessages(e)
	if items, _ := m.Map(harness.Record{Data: doc}); len(items) == 0 {
		t.Fatal("first session not followed")
	}
	if items, _ := m.Map(harness.Record{Data: other}); len(items) != 0 || m.State()["harness_session"] != fixtureID {
		t.Fatalf("switched without evidence: %v", m.State())
	}
}
