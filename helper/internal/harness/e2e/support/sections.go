package support

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/ports"
)

// The Changes and Ports sections are filled by the engine, not by an
// agent's files: Changes from git's view of the session's folder (at the
// start, at each turn's end and every 30 seconds), Ports from the session's
// process tree (at the start and every 15 seconds). What belongs to an
// agent is that its adapter declares the capability, that its turn end
// reaches the engine, and what it names as its edits. The scenarios here
// run the engine's side for one agent through the agent's own turns.

// GitIn runs git in dir with no user or system configuration.
func GitIn(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t testing.TB, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// GitTree makes dir a repository with one file of each kind of change:
// e0.go committed and edited, d0.go committed and deleted, a1.go new and
// staged, e1.go new and not added.
func GitTree(t testing.TB, dir string) {
	t.Helper()
	GitIn(t, dir, "init", "-q")
	put(t, dir, "e0.go", "package a\n")
	put(t, dir, "d0.go", "package a\n")
	GitIn(t, dir, "add", ".")
	GitIn(t, dir, "commit", "-qm", "first")
	put(t, dir, "e0.go", "package b\n")
	if err := os.Remove(filepath.Join(dir, "d0.go")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "a1.go", "package c\n")
	GitIn(t, dir, "add", "a1.go")
	put(t, dir, "e1.go", "package d\n")
}

// Rows lists a mission section's rows.
func Rows(st enginetest.State, kind string) []harness.Row {
	for _, s := range st.Mission.Sections {
		if s.Kind == kind {
			return s.Rows
		}
	}
	return nil
}

// ChangesAre is true when the Changes section lists exactly these files
// (path to the word the row shows), each opening its diff.
func ChangesAre(want map[string]string) func(enginetest.State) bool {
	return func(st enginetest.State) bool {
		rows := Rows(st, harness.SectionChanges)
		if len(rows) != len(want) {
			return false
		}
		for _, r := range rows {
			if want[r.ID] != r.Subtitle || r.Title != r.ID || r.State != harness.StateOK ||
				r.Open == nil || r.Open.Kind != harness.OpenDiff || r.Open.Ref != r.ID {
				return false
			}
		}
		return true
	}
}

// ChangesAtStart is what GitTree leaves.
var ChangesAtStart = map[string]string{"e0.go": "modified", "d0.go": "deleted", "a1.go": "added", "e1.go": "new"}

// ChangesAfter is what the tree reads after ChangesScenario's edit.
var ChangesAfter = map[string]string{"e0.go": "modified", "d0.go": "deleted", "a1.go": "added", "e2.go": "new"}

// ChangesScenario checks the Changes section of a session whose folder
// is the repository GitTree made, before the rig was started: the section
// shows each file with what happened to it. The tree then changes (e1.go
// is deleted, e2.go is new) and next makes the agent end another turn,
// returning what shows it; the section follows the tree once that turn
// ends.
func ChangesScenario(t *testing.T, r *enginetest.Rig, cwd string, next func() func(enginetest.State) bool) enginetest.State {
	t.Helper()
	WaitFor(t, r, "the changes at the start", ChangesAre(ChangesAtStart))
	if err := os.Remove(filepath.Join(cwd, "e1.go")); err != nil {
		t.Fatal(err)
	}
	put(t, cwd, "e2.go", "package e\n")
	seen := next()
	st := WaitFor(t, r, "the turn", seen)
	// The turn has ended. Its end refreshes the section within moments; the
	// 30 second refresh is far enough off that waiting for it fails.
	deadline := time.Now().Add(turnRefresh)
	for !ChangesAre(ChangesAfter)(st) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		st = r.Read()
	}
	if !ChangesAre(ChangesAfter)(st) {
		t.Fatalf("changes %v after the turn ended: %+v, want %v", turnRefresh, Rows(st, harness.SectionChanges), ChangesAfter)
	}
	return st
}

// Dev is the listeners the Ports scenario scripts: a web server and a
// bundler's dev server, started by the agent.
var Dev = []ports.Listener{
	{Port: 3000, Address: "127.0.0.1", PID: 4101, Command: "node"},
	{Port: 5173, Address: "127.0.0.1", PID: 4102, Command: "vite"},
}

// PortsAre is true when the Ports section lists exactly these ports, each
// running, titled with the port, naming its program and opening as a URL.
func PortsAre(want map[string]string) func(enginetest.State) bool {
	return func(st enginetest.State) bool {
		rows := Rows(st, harness.SectionPorts)
		if len(rows) != len(want) {
			return false
		}
		for _, r := range rows {
			if cmd, ok := want[r.ID]; !ok || r.Subtitle != cmd || r.Title != ":"+r.ID || r.State != harness.StateRunning ||
				r.Open == nil || r.Open.Kind != harness.OpenURL || r.Open.Ref != r.ID {
				return false
			}
		}
		return true
	}
}

// PortsScenario checks the Ports section of a session whose rig has the
// Dev listeners (Options.Ports): both ports show with their programs.
// The server on 5173 then stops and the helper restarts: the section
// lists only 3000, and a server that starts later (8080) shows after the
// next restart. extra is what the rig lists besides these (an agent's own
// server).
func PortsScenario(t *testing.T, r *enginetest.Rig, extra map[string]string, then func(enginetest.State) bool) {
	t.Helper()
	with := func(m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range extra {
			out[k] = v
		}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	ok := func(want map[string]string) func(enginetest.State) bool {
		return func(st enginetest.State) bool { return PortsAre(want)(st) && (then == nil || then(st)) }
	}
	both := with(map[string]string{"3000": "node", "5173": "vite"})
	WaitFor(t, r, "both ports", ok(both))
	r.SetListeners(Dev[0])
	r.Restart()
	one := with(map[string]string{"3000": "node"})
	WaitFor(t, r, "the port of the server that stopped to leave", ok(one))
	r.SetListeners(Dev[0], ports.Listener{Port: 8080, Address: "127.0.0.1", PID: 4103, Command: "python"})
	r.Restart()
	later := with(map[string]string{"3000": "node", "8080": "python"})
	WaitFor(t, r, "the port of the server that started", ok(later))
}

// turnRefresh is how long after a turn's end the Changes section may take
// to follow the tree: git status in a small repository.
const turnRefresh = 10 * time.Second
