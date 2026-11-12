package source

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// fakeProcs is an injected process view: a fixed tree and a table of who
// holds which file open, changeable while a watcher runs.
type fakeProcs struct {
	mu      sync.Mutex
	tree    []int
	holders map[string][]int
	others  map[string]bool // cwd -> a foreign agent process runs there
	err     error
}

func (f *fakeProcs) Others(_ context.Context, _ []string, cwd string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.others[cwd], f.err
}

func (f *fakeProcs) Tree(context.Context) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.tree...)
}

func (f *fakeProcs) Holders(_ context.Context, paths []string) (map[string][]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]int{}
	for _, p := range paths {
		if h := f.holders[p]; len(h) > 0 {
			out[p] = h
		}
	}
	return out, nil
}

func (f *fakeProcs) hold(path string, pids ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holders == nil {
		f.holders = map[string][]int{}
	}
	f.holders[path] = pids
}

func exclusiveSpec() harness.SourceSpec {
	return sspec("t", map[string]any{"glob": "~/*.jsonl", "follow": true, "exclusive": true})
}

func writeLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// (a) A file a process outside the session's tree holds open is never
// taken, however new it is.
func TestExclusiveIgnoresForeignProcessFile(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID = "s_foreign001"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	procs := &fakeProcs{tree: []int{100, 101}}
	env.Procs = procs
	own, foreign := filepath.Join(h, "own.jsonl"), filepath.Join(h, "foreign.jsonl")
	writeLine(t, own, `{"f":"own1"}`)
	procs.hold(own, 101)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	procs.hold(foreign, 999)
	writeLine(t, foreign, `{"f":"foreign1"}`)
	time.Sleep(300 * time.Millisecond)
	writeLine(t, own, `{"f":"own2"}`)
	got := c.wait(t, 2)
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 2 || got[0] != `{"f":"own1"}` || c.recs[1] != `{"f":"own2"}` {
		t.Fatalf("followed a foreign process's file: %v", c.recs)
	}
}

// (b) The session's own agent opening a new file (a /new) moves the
// session there and drops the old file, so records do not interleave.
func TestExclusiveFollowsOwnProcessToNewFile(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID = "s_newfile0001"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	procs := &fakeProcs{tree: []int{100, 101}}
	env.Procs = procs
	oldF, newF := filepath.Join(h, "old.jsonl"), filepath.Join(h, "new.jsonl")
	writeLine(t, oldF, `{"f":"old1"}`)
	procs.hold(oldF, 101)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	procs.hold(oldF)
	procs.hold(newF, 101)
	writeLine(t, newF, `{"f":"new1"}`)
	c.wait(t, 2)
	writeLine(t, oldF, `{"f":"old2"}`) // stray write must no longer be followed
	time.Sleep(300 * time.Millisecond)
	writeLine(t, newF, `{"f":"new2"}`)
	c.wait(t, 3)
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	want := []string{`{"f":"old1"}`, `{"f":"new1"}`, `{"f":"new2"}`}
	if len(c.recs) != 3 || c.recs[1] != want[1] || c.recs[2] != want[2] {
		t.Fatalf("records = %v, want %v", c.recs, want)
	}
}

// (c) Two Togo sessions in one folder each keep to their own agent's file.
func TestExclusiveTwoSessionsEachGetOwnFile(t *testing.T) {
	envA, h := testEnv(t)
	envA.SessionID = "s_twoaaaaa01"
	created := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	envA.Vars["created"] = created
	envB := envA
	envB.Vars = map[string]string{VarHome: h, "created": created}
	envB.SessionID = "s_twobbbbb01"
	pa, pb := &fakeProcs{tree: []int{10, 11}}, &fakeProcs{tree: []int{20, 21}}
	envA.Procs, envB.Procs = pa, pb
	fa, fb := filepath.Join(h, "a.jsonl"), filepath.Join(h, "b.jsonl")
	// Both files exist and both are newer than either session: only the
	// process evidence tells them apart.
	writeLine(t, fa, `{"f":"a1"}`)
	writeLine(t, fb, `{"f":"b1"}`)
	for _, p := range []*fakeProcs{pa, pb} {
		p.hold(fa, 11)
		p.hold(fb, 21)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := &collector{}, &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), envA, a.emit) }()
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), envB, b.emit) }()
	ga, gb := a.wait(t, 1), b.wait(t, 1)
	time.Sleep(200 * time.Millisecond)
	a.mu.Lock()
	b.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	if len(a.recs) != 1 || len(b.recs) != 1 || ga[0] != `{"f":"a1"}` || gb[0] != `{"f":"b1"}` {
		t.Fatalf("A = %v, B = %v", a.recs, b.recs)
	}
}

// With nothing holding a new file there is no evidence it is ours: a
// session that has a file keeps it.
func TestExclusiveKeepsFileWithoutEvidence(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID = "s_noevid0001"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	procs := &fakeProcs{tree: []int{100}, others: map[string]bool{env.Cwd: true}}
	env.Procs = procs
	first, other := filepath.Join(h, "first.jsonl"), filepath.Join(h, "other.jsonl")
	writeLine(t, first, `{"f":"first1"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	writeLine(t, other, `{"f":"other1"}`)
	time.Sleep(300 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 1 {
		t.Fatalf("switched without evidence: %v", c.recs)
	}
}

// (e) After a helper restart, an idle session whose own file nobody holds
// still finds it when a newer file in the folder belongs to a foreign
// process: the newest file the session may claim is its own, not none.
func TestExclusiveIdleOwnFileBehindForeignNewer(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID = "s_idleown001"
	env.Vars["created"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	procs := &fakeProcs{tree: []int{100, 101}}
	env.Procs = procs
	own, foreign := filepath.Join(h, "own.jsonl"), filepath.Join(h, "foreign.jsonl")
	writeLine(t, own, `{"f":"own1"}`)
	past := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(own, past, past); err != nil {
		t.Fatal(err)
	}
	writeLine(t, foreign, `{"f":"foreign1"}`)
	procs.hold(foreign, 999)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	got := c.wait(t, 1)
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 1 || got[0] != `{"f":"own1"}` {
		t.Fatalf("idle own file not found behind a foreign one: %v", c.recs)
	}
}

// Pi appends and closes its file per entry, so nobody holds a new file
// open. With no other agent of the same kind in the folder, a new file is
// the session's own /new; the old file is dropped.
func TestExclusiveUnheldNewFileIsOwnNewWhenNoForeignAgent(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID, env.Agent = "s_unheld0001", "pi"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	env.Procs = &fakeProcs{tree: []int{100}}
	oldF, newF := filepath.Join(h, "old.jsonl"), filepath.Join(h, "new.jsonl")
	writeLine(t, oldF, `{"f":"old1"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	writeLine(t, newF, `{"f":"new1"}`)
	c.wait(t, 2)
	writeLine(t, oldF, `{"f":"old2"}`)
	time.Sleep(300 * time.Millisecond)
	writeLine(t, newF, `{"f":"new2"}`)
	c.wait(t, 3)
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 3 || c.recs[1] != `{"f":"new1"}` || c.recs[2] != `{"f":"new2"}` {
		t.Fatalf("records = %v", c.recs)
	}
}

// Another agent of the same kind in the same folder makes an unheld new
// file ambiguous: the session keeps its file. In another folder it does
// not matter. A lookup that fails is ambiguous too.
func TestExclusiveUnheldNewFileWithForeignAgent(t *testing.T) {
	cases := []struct {
		name    string
		others  func(cwd string) map[string]bool
		err     error
		follows bool
	}{
		{"same folder", func(cwd string) map[string]bool { return map[string]bool{cwd: true} }, nil, false},
		{"other folder", func(string) map[string]bool { return map[string]bool{"/elsewhere": true} }, nil, true},
		{"lookup fails", func(string) map[string]bool { return nil }, context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, h := testEnv(t)
			env.SessionID, env.Agent = "s_amb0000001", "omp"
			env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			env.Procs = &fakeProcs{tree: []int{100}, others: tc.others(env.Cwd), err: tc.err}
			oldF, newF := filepath.Join(h, "old.jsonl"), filepath.Join(h, "new.jsonl")
			writeLine(t, oldF, `{"f":"old1"}`)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &collector{}
			go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
			c.wait(t, 1)
			time.Sleep(60 * time.Millisecond)
			writeLine(t, newF, `{"f":"new1"}`)
			time.Sleep(400 * time.Millisecond)
			c.mu.Lock()
			defer c.mu.Unlock()
			if followed := len(c.recs) == 2; followed != tc.follows {
				t.Fatalf("follows = %v, want %v: %v", followed, tc.follows, c.recs)
			}
		})
	}
}
