package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/omp"
)

// Two real omp conversations: one recorded on 19.0.1, one on 18.x.
const (
	ompConvA     = "01a1f71a-7ab0-7783-8475-8f8da027ec20" // session-19.jsonl
	ompConvAFile = "2026-11-20T03-57-50-128Z_" + ompConvA + ".jsonl"
	ompConvAText = "Reply with the single word: pear"
	ompConvB     = "0b7c1f2a9e" // session.jsonl
	ompConvBFile = "2026-11-06T10-00-00-000Z_" + ompConvB + ".jsonl"
	ompConvBText = "Fix the failing parser test"
)

// ompRig is the real omp adapter over a temporary home: the session files
// omp writes, the per-terminal records it keeps, and a tmux pane whose
// terminal device changes when the pane is respawned, as tmux does.
type ompRig struct {
	t       *testing.T
	e       *Engine
	id      string
	home    string
	cwd     string
	dir     string // omp's sessions folder for cwd
	records string // omp's terminal-sessions folder

	mu       sync.Mutex
	tty      string
	next     []string // devices the next respawns get
	argv     [][]string
	onRelaun func(argv []string, tty string)
	status   string
}

// setStatus is the hub's status of the session, and tells the engine as
// the hub's session event does.
func (r *ompRig) setStatus(status string) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
	r.e.mu.Lock()
	s := r.e.sessions[r.id]
	r.e.mu.Unlock()
	s.statusChanged()
}

func newOmpRig(t *testing.T, id string) *ompRig {
	t.Helper()
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	r := &ompRig{t: t, id: id, home: home, cwd: cwd, tty: "/dev/ttys011", status: "idle",
		records: filepath.Join(home, ".omp", "agent", "terminal-sessions")}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	// The folder omp names for cwd (a temporary folder gets its own form).
	r.dir = filepath.Join(home, ".omp", "agent", "sessions", omp.SessionDirNames(cwd, home, os.Getenv("TMPDIR"))[0])
	for _, d := range []string{r.dir, r.records} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.copyFixture("session-19.jsonl", ompConvAFile)
	r.copyFixture("session.jsonl", ompConvBFile)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "omp"), []byte("#!/bin/sh\n[ \"$1\" = --version ] && echo omp/19.0.1 && exit 0\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pane := &fakePane{mode: "a"}
	r.e = New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "omp", Managed: true, Cwd: cwd, Command: []string{"omp"},
			CreatedAt: time.Now().Add(-time.Hour)}},
		Typist: pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		PaneTTY: func(context.Context, string) (string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.tty, nil
		},
		Status: func(string) string {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.status
		},
		Relaunch: func(_ context.Context, _ string, argv []string) error {
			r.respawn(argv)
			return nil
		},
		Log:   &Log{NewEpoch: newEpochCounter()},
		Scale: 0.05,
	})
	t.Cleanup(func() { r.e.Forget(id) })
	return r
}

func newEpochCounter() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return "e" + strings.Repeat("i", n)
	}
}

func (r *ompRig) copyFixture(name, as string) {
	b, err := os.ReadFile(filepath.Join("..", "adapters", "omp", "testdata", name))
	if err != nil {
		r.t.Fatal(err)
	}
	r.writeConversation(as, b)
}

// writeConversation saves a conversation file written before the session
// started, as the ones a resume picks are.
func (r *ompRig) writeConversation(as string, b []byte) {
	path := filepath.Join(r.dir, as)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		r.t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		r.t.Fatal(err)
	}
}

// respawn plays tmux respawn-pane: the pane gets the next free device,
// and the agent that starts there runs onRelaun.
func (r *ompRig) respawn(argv []string) {
	r.mu.Lock()
	r.argv = append(r.argv, argv)
	if len(r.next) > 0 {
		r.tty, r.next = r.next[0], r.next[1:]
	}
	tty, f := r.tty, r.onRelaun
	r.mu.Unlock()
	if f != nil {
		f(argv, tty)
	}
}

// record writes omp's terminal record for a device, naming a session file.
func (r *ompRig) record(tty, file string) {
	r.t.Helper()
	name := strings.ReplaceAll(strings.TrimPrefix(tty, "/dev/"), "/", "-")
	body := r.cwd + "\n" + filepath.Join(r.dir, file) + "\ncwdstat 1 2\n"
	if err := os.WriteFile(filepath.Join(r.records, name), []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *ompRig) start() {
	r.t.Helper()
	if _, err := r.e.Describe(context.Background(), r.id); err != nil {
		r.t.Fatal(err)
	}
}

// users is the user messages the chat holds now.
func (r *ompRig) users() []string {
	items, _, _ := r.e.log.Since(r.id, 0, 500)
	var out []string
	for _, it := range items {
		if it.Kind == harness.KindUser && it.User != nil && it.Parent == "" {
			out = append(out, it.User.Text)
		}
	}
	return out
}

func (r *ompRig) shows(text string) bool {
	for _, u := range r.users() {
		if strings.HasPrefix(u, text) {
			return true
		}
	}
	return false
}

// waitShowsOnly waits until the chat holds text and nothing of other.
func (r *ompRig) waitShowsOnly(text, other string) {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !(r.shows(text) && !r.shows(other)) {
		if time.Now().After(deadline) {
			r.t.Fatalf("chat holds %q, want %q and nothing of %q", r.users(), text, other)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *ompRig) resume(conv string) harness.ActionResult {
	r.t.Helper()
	raw, _ := json.Marshal(conv)
	res, err := r.e.Act(context.Background(), harness.ActionRequest{Session: r.id, Action: "resume",
		Args: map[string]json.RawMessage{"id": raw}})
	if err != nil {
		r.t.Fatalf("resume: %v", err)
	}
	return res
}

// Seen live on omp 19.0.1: a resume from the phone relaunches omp in its
// pane, tmux gives the pane a new terminal device, and omp writes its
// record under that device. The helper kept reading the old device's
// record, which still named the conversation before the resume, so the
// chat stayed there and the next reply never reached it.
func TestOmpResumeFromPhoneFollowsTheResumedConversation(t *testing.T) {
	r := newOmpRig(t, "s_ompresume01")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012"}
	r.onRelaun = func(_ []string, tty string) { r.record(tty, ompConvBFile) }
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)

	if res := r.resume(ompConvB); res.Result != harness.ResultRelaunched {
		t.Fatalf("resume = %+v", res)
	}
	// The old device's record is still on disk, naming the old file.
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

// A resume of the conversation the chat already shows does not start the
// chat over.
func TestOmpResumeOfTheShownConversationKeepsTheChat(t *testing.T) {
	r := newOmpRig(t, "s_ompresume02")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012"}
	r.onRelaun = func(_ []string, tty string) { r.record(tty, ompConvAFile) }
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	epoch := r.e.log.Epoch(r.id)
	if res := r.resume(ompConvA); res.Result != harness.ResultRelaunched {
		t.Fatalf("resume = %+v", res)
	}
	// Settled once the session reads the new device's record.
	waitFor(t, func() bool {
		return r.e.stateOf(r.id)["session_rel"] == filepath.Base(r.dir)+"/"+ompConvAFile && r.relaunchedVar() != ""
	})
	r.waitShowsOnly(ompConvAText, ompConvBText)
	if got := r.e.log.Epoch(r.id); got != epoch {
		t.Fatalf("epoch %q, was %q: the chat started over on the same conversation", got, epoch)
	}
}

// A resume that omp does not complete (it exits on an unknown id, or shows
// its own chooser) writes no record for the new device: the chat is not
// moved by the old device's record, and moves once a conversation is
// picked in the chooser.
func TestOmpResumeNotCompletedThenPickedAtTheDesk(t *testing.T) {
	r := newOmpRig(t, "s_ompresume03")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012"}
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	if res := r.resume(ompConvB); res.Result != harness.ResultRelaunched {
		t.Fatalf("resume = %+v", res)
	}
	// The old device's record, naming A, is no longer the session's.
	waitFor(t, func() bool {
		st := r.e.stateOf(r.id)
		return st["session_dir"] != "" && st["session_rel"] == st["session_dir"]+"/*.jsonl"
	})
	if !r.shows(ompConvAText) || r.shows(ompConvBText) {
		t.Fatalf("chat holds %q, want it kept on the conversation before the resume", r.users())
	}
	r.record("/dev/ttys012", ompConvBFile)
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

func (r *ompRig) relaunchedVar() string {
	r.e.mu.Lock()
	s := r.e.sessions[r.id]
	r.e.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mapperEnvLocked().Vars["relaunched"]
}

func (e *Engine) stateOf(id string) map[string]string {
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}
