package omp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

func crumbEnv(t *testing.T) (harness.Env, string) {
	t.Helper()
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return harness.Env{Cwd: cwd, Vars: map[string]string{source.VarHome: home, source.VarTTY: "/dev/ttys011"}}, home
}

func writeCrumb(t *testing.T, home, tty, body string) {
	t.Helper()
	d := filepath.Join(home, ".omp", "agent", "terminal-sessions")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, tty), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// omp records, per terminal, the session file it uses: a /resume or /new
// in the session's pane moves the chat to that file at once.
func TestTerminalSessionFollowsOmpsRecord(t *testing.T) {
	env, home := crumbEnv(t)
	file := filepath.Join(home, ".omp", "agent", "sessions", "-project", "2026-11-12T15-57-52-145Z_abc.jsonl")
	writeCrumb(t, home, "ttys011", env.Cwd+"\n"+file+"\ncwdstat 1 2")
	if got := terminalSession(env); got != "-project/2026-11-12T15-57-52-145Z_abc.jsonl" {
		t.Fatalf("terminalSession = %q", got)
	}
	s := newSession(env)
	st := s.State()
	if st["session_rel"] != "-project/2026-11-12T15-57-52-145Z_abc.jsonl" || st["subagent_rel"] != "-project/2026-11-12T15-57-52-145Z_abc/*.jsonl" {
		t.Fatalf("state = %v", st)
	}
}

// A record for another folder, outside omp's sessions, or for a terminal
// that is not the pane's, is not the session's.
func TestTerminalSessionRejectsOtherRecords(t *testing.T) {
	env, home := crumbEnv(t)
	root := filepath.Join(home, ".omp", "agent", "sessions")
	cases := map[string]string{
		"other folder":     "/somewhere/else\n" + filepath.Join(root, "-x", "a.jsonl"),
		"outside sessions": env.Cwd + "\n" + filepath.Join(home, "elsewhere.jsonl"),
		"escapes":          env.Cwd + "\n" + filepath.Join(root, "..", "..", "x.jsonl"),
		"not jsonl":        env.Cwd + "\n" + filepath.Join(root, "-project", "a.txt"),
		"empty":            "",
	}
	for name, body := range cases {
		writeCrumb(t, home, "ttys011", body)
		if got := terminalSession(env); got != "" {
			t.Errorf("%s: terminalSession = %q, want none", name, got)
		}
	}
	writeCrumb(t, home, "ttys012", env.Cwd+"\n"+filepath.Join(root, "-project", "a.jsonl"))
	_ = os.Remove(filepath.Join(home, ".omp", "agent", "terminal-sessions", "ttys011"))
	if got := terminalSession(env); got != "" {
		t.Fatalf("another terminal's record was used: %q", got)
	}
}

func TestTerminalSessionRejectsRecordFromReusedTerminal(t *testing.T) {
	env, home := crumbEnv(t)
	created := time.Now().UTC().Truncate(time.Second)
	env.Vars["created"] = created.Format(time.RFC3339)
	file := filepath.Join(home, ".omp", "agent", "sessions", "-project", "previous.jsonl")
	writeCrumb(t, home, "ttys011", env.Cwd+"\n"+file+"\nfresh\ncwdstat 1 2")
	record := filepath.Join(home, ".omp", "agent", "terminal-sessions", "ttys011")
	previous := created.Add(-time.Minute)
	if err := os.Chtimes(record, previous, previous); err != nil {
		t.Fatal(err)
	}
	if got := terminalSession(env); got != "" {
		t.Fatalf("new session followed the previous terminal occupant: %q", got)
	}
	if got := newSession(env).State()["session_rel"]; got == "-project/previous.jsonl" {
		t.Fatal("stale terminal record selected as the new session's feed")
	}
}

func TestTerminalSessionKeepsResumedRecordAcrossHelperRestart(t *testing.T) {
	env, home := crumbEnv(t)
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	env.Vars["created"] = created.Format(time.RFC3339)
	file := filepath.Join(home, ".omp", "agent", "sessions", "-project", "2026-01-01T00-00-00-000Z_resumed.jsonl")
	writeCrumb(t, home, "ttys011", env.Cwd+"\n"+file+"\ncwdstat 1 2")
	record := filepath.Join(home, ".omp", "agent", "terminal-sessions", "ttys011")
	resumed := created.Add(time.Minute)
	if err := os.Chtimes(record, resumed, resumed); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if got := newSession(env).State()["session_rel"]; got != "-project/2026-01-01T00-00-00-000Z_resumed.jsonl" {
			t.Fatalf("helper instance %d lost the active resumed conversation: %q", i, got)
		}
	}
}

// Without a record the folder's files are watched as before, and the
// subagent folder waits for the session's id.
func TestSessionRelFallsBackToFolder(t *testing.T) {
	env, _ := crumbEnv(t)
	delete(env.Vars, source.VarTTY)
	s := newSession(env)
	st := s.State()
	if st["session_rel"] != st["session_dir"]+"/*.jsonl" || st["subagent_rel"] != "" {
		t.Fatalf("state = %v", st)
	}
	s.state["harness_session"] = "abc"
	if st := s.State(); st["subagent_rel"] != st["session_dir"]+"/*_abc/*.jsonl" {
		t.Fatalf("state = %v", st)
	}
}
