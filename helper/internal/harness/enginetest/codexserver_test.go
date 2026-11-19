package enginetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

func codexSession(t *testing.T, turns ...sessiongen.Turn) (*Rig, *sessiongen.Codex, *sessiongen.Layout) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &sessiongen.Codex{}
	start := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	l, err := sessiongen.Write(home, p, sessiongen.Script{Seed: 1, Start: start, Cwd: cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	r := New(t, Options{Agent: "codex", Home: home, Cwd: cwd, ID: NewID(), Created: start.Add(-time.Minute), NoRedact: true})
	return r, p, l
}

// The engine starts the app-server through the fake codex program, the
// server listens on the socket the engine picked, and the helper joins
// the thread as a client: the history arrives, the terminal is given
// --remote, every message and answer the server sent is one a recording
// holds, and a helper restart finds the same server again.
func TestCodexServerThroughTheEngine(t *testing.T) {
	r, p, l := codexSession(t, sessiongen.Turn{Say: 40, Tools: 2, Output: 60},
		sessiongen.Turn{Say: 30, Agents: &sessiongen.Batch{Size: 1, Steps: 1}})
	s := r.Codex()
	if args := r.CodexLaunch(); len(args) != 2 || args[0] != "--remote" || args[1] != "unix://"+s.Path() {
		t.Fatalf("launch args %q, socket %q", args, s.Path())
	}
	r.SettleUntil(30*time.Second, Has(l.Truth.Last))
	if s.Conns() < 1 {
		t.Error("the helper is not connected")
	}
	var methods []string
	for _, c := range s.Calls() {
		methods = append(methods, c.Method)
	}
	for _, m := range []string{"initialize", "thread/loaded/list", "thread/resume", "account/rateLimits/read"} {
		if !slices.Contains(methods, m) {
			t.Errorf("the helper never called %s: %v", m, methods)
		}
	}
	shapes, err := sessiongen.LoadCodexShapes(filepath.Join("..", "adapters", "codex", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	res, _ := s.fold.resume(p.Thread())
	s.mu.Unlock()
	for _, d := range shapes.Check(map[string]any{"id": 1, "result": res}) {
		t.Errorf("resume result: %s", d)
	}
	pids := r.AppServerPIDs()
	before := len(s.CallsOf("thread/resume"))
	r.Restart()
	r.SettleUntil(30*time.Second, Has(l.Truth.Last))
	if got := r.AppServerPIDs(); !slices.Equal(got, pids) {
		t.Errorf("the restarted helper started another app-server: %v, was %v", got, pids)
	}
	eventually(t, 30*time.Second, func() bool { return len(s.CallsOf("thread/resume")) > before })
}

// A call told to fail answers an error, a slow one waits, and a request
// answered at the desk is resolved once.
func TestCodexServerKnobs(t *testing.T) {
	r, p, l := codexSession(t, sessiongen.Turn{Say: 10})
	s := r.Codex()
	// The engine joins the thread on its own clock: act only once it has
	// resumed the thread and shown its history.
	r.SettleUntil(30*time.Second, Has(l.Truth.Last))
	eventually(t, 30*time.Second, func() bool { return len(s.CallsOf("thread/resume")) > 0 })
	s.Append(sessiongen.CodexFileApproval(7, p.Thread(), "turn-x", "item-x", time.Now()))
	s.Flush()
	if len(s.Pending()) != 1 {
		t.Fatalf("pending %+v", s.Pending())
	}
	if !s.AnswerAtDesk(7) || s.AnswerAtDesk(7) {
		t.Error("a desk answer is taken once")
	}
	s.Fail("thread/compact/start", -32600, "busy", 1)
	res, err := r.Act("compact", "", map[string]any{})
	if err != nil || res.Result != "failed" {
		t.Errorf("a failed compact: %+v %v", res, err)
	}
	var p2 struct {
		ThreadID string `json:"threadId"`
	}
	calls := s.CallsOf("thread/compact/start")
	if len(calls) != 1 || json.Unmarshal(calls[0].Params, &p2) != nil || p2.ThreadID != p.Thread() {
		t.Errorf("compact calls %+v", calls)
	}
}

func eventually(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()
	limit = max(limit, MinSettleLimit)
	for deadline := time.Now().Add(limit); !ok(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
	}
}
