package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The suite restores a watched config only when every change since the
// snapshot happened while the suite itself was driving that harness. Each
// harness run observes its watched files when it starts, before every call
// it makes and when it ends. A change seen at an observation is the suite's
// when the observation before it was taken inside the run (after its first
// call); a change before the first call, after the run ended, or seen only
// at restore time is someone else's (the owner signing in, another
// instance writing its state), and the file is then left as it is.
//
// The ledger is kept in the snapshot folder too, so -cleanup after a crash
// judges by the same record; with no record a changed file is left alone.

type track struct {
	Last    string `json:"last"`    // hash at the latest observation ("" = absent)
	Active  bool   `json:"active"`  // latest observation was inside the suite's run
	Suite   int    `json:"suite"`   // changes seen inside the run
	Outside int    `json:"outside"` // changes seen outside it
}

type ledger struct {
	mu     sync.Mutex
	dir    string
	tracks map[string]*track
}

var owned = &ledger{tracks: map[string]*track{}}

// start begins the ledger from the snapshot's hashes.
func (l *ledger) start(dir string, entries []snapEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dir = dir
	for _, e := range entries {
		l.tracks[e.Path] = &track{Last: e.Sum}
	}
	l.saveLocked()
}

// load reads a ledger an earlier process wrote into dir.
func (l *ledger) load(dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dir = dir
	if b, err := os.ReadFile(filepath.Join(dir, "ledger.json")); err == nil {
		_ = json.Unmarshal(b, &l.tracks)
	}
}

func fileSum(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return sum(b)
}

// observe hashes the harness's watched files. inside says whether the suite
// is driving the harness from this observation on.
func (l *ledger) observe(harness string, inside bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	for _, w := range watched[harness] {
		p := expand(w.path)
		t, ok := l.tracks[p]
		if !ok {
			continue
		}
		if s := fileSum(p); s != t.Last {
			if t.Active {
				t.Suite++
			} else {
				t.Outside++
			}
			t.Last, changed = s, true
		}
		if t.Active != inside {
			t.Active, changed = inside, true
		}
	}
	if changed {
		l.saveLocked()
	}
}

func (l *ledger) saveLocked() {
	if l.dir == "" {
		return
	}
	b, _ := json.MarshalIndent(l.tracks, "", "  ")
	_ = os.WriteFile(filepath.Join(l.dir, "ledger.json"), b, 0o600)
}

// ownChange reports whether the file's current bytes (hash cur) are the
// suite's own doing, and if not, why.
func (l *ledger) ownChange(p, cur string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.tracks[p]
	switch {
	case !ok:
		return false, "no record of the suite's own changes"
	case t.Outside > 0:
		return false, fmt.Sprintf("changed %d time(s) while the suite was not driving it", t.Outside)
	case cur != t.Last:
		return false, "changed after the suite's last action"
	case t.Suite == 0:
		return false, "no change seen during the suite's actions"
	}
	return true, ""
}

// backup copies the current bytes next to the file before a restore.
func backup(p string, b []byte) (string, error) {
	dst := fmt.Sprintf("%s.togo-conformance-backup-%s", p, time.Now().Format("20060102-150405"))
	return dst, os.WriteFile(dst, b, 0o600)
}
