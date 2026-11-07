package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// `claude agents --json` lists every Claude Code session on the machine,
// Togo's or not, with its folder. No section of one session's mission may
// be filled from it: that showed a neighbour session's name and folder
// under Background.
func TestMissionReadsNothingMachineWide(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	for section, m := range a.Mission {
		t.Errorf("mission section %s is filled from source %q; only this session's transcript may fill it", section, m.Source)
	}
	for name, s := range a.Sources {
		if s.CLI != nil && slices.Contains(s.CLI.Argv, "agents") {
			t.Errorf("source %s runs %v, which lists other sessions", name, s.CLI.Argv)
		}
	}
}

func transcriptEnv(t *testing.T, created time.Time) (env func() map[string]string, dir string) {
	t.Helper()
	home := t.TempDir()
	key := "-HOME-proj"
	dir = filepath.Join(home, ".claude", "projects", key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return func() map[string]string {
		return map[string]string{source.VarHome: home, "cwd_key": key, "created": created.UTC().Format(time.RFC3339)}
	}, dir
}

func writeAt(t *testing.T, p string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// Before a SessionStart hook arrives the folder's transcript stands in, but
// only when it can only be this session's.
func TestTranscriptGuessNeverPicksANeighbour(t *testing.T) {
	created := time.Now().Add(-10 * time.Minute)
	vars, dir := transcriptEnv(t, created)
	e := env()
	e.Vars = vars()
	e.Cwd = filepath.Dir(dir)

	old := filepath.Join(dir, "old.jsonl")
	writeAt(t, old, created.Add(-time.Hour))
	if got := transcriptFor(e); got != "" {
		t.Fatalf("guessed %s, a transcript from before the session started", got)
	}

	mine := filepath.Join(dir, "mine.jsonl")
	writeAt(t, mine, created.Add(time.Minute))
	if got := transcriptFor(e); filepath.Base(got) != "mine.jsonl" {
		t.Fatalf("guessed %q, want the one transcript written since the session started", got)
	}

	// Another Claude Code session starts in the same folder and writes after ours.
	writeAt(t, filepath.Join(dir, "neighbour.jsonl"), created.Add(2*time.Minute))
	if got := transcriptFor(e); got != "" {
		t.Fatalf("guessed %s with two sessions writing in the folder", got)
	}

	e.Vars["created"] = ""
	if got := transcriptFor(e); got != "" {
		t.Fatalf("guessed %s without knowing when the session started", got)
	}
}

// A hook names the transcript outright, and the guess never replaces it.
func TestHookTranscriptWinsOverTheGuess(t *testing.T) {
	created := time.Now().Add(-10 * time.Minute)
	vars, dir := transcriptEnv(t, created)
	writeAt(t, filepath.Join(dir, "neighbour.jsonl"), created.Add(time.Minute))
	e := env()
	e.Vars = vars()
	h := newHooks(e)
	h.Map(hookRecord(t, `{"hook_event_name":"SessionStart","session_id":"mine","transcript_path":"/HOME/mine.jsonl"}`))
	if got := h.State()["transcript_path"]; got != "/HOME/mine.jsonl" {
		t.Fatalf("transcript_path = %q, want the hook's", got)
	}
}

func hookRecord(t *testing.T, payload string) harness.Record {
	t.Helper()
	return harness.Record{Source: "hooks", At: time.Now(), Data: json.RawMessage(payload)}
}
