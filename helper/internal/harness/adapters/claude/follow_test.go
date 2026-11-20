package claude

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// followSetup is a session folder holding two real transcripts written
// since the session started, as after a /clear: the folder guess cannot
// tell them apart.
func followSetup(t *testing.T) (harness.Env, string, string) {
	t.Helper()
	created := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	vars, dir := transcriptEnv(t, created)
	dir, err := filepath.EvalSymlinks(dir) // the guess reports resolved paths
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "transcript-basic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Written since the session started, as the guess requires.
	stamp := `"timestamp":"` + created.Add(30*time.Second).UTC().Format(time.RFC3339Nano) + `"`
	raw = regexp.MustCompile(`"timestamp":"[^"]*"`).ReplaceAll(raw, []byte(stamp))
	first := filepath.Join(dir, "0b6e5f8e-1111-4222-8333-944455556666.jsonl")
	second := filepath.Join(dir, "7c1d2e3f-aaaa-4bbb-8ccc-ddddeeeeffff.jsonl")
	for i, p := range []string{first, second} {
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		at := created.Add(time.Duration(i+1) * time.Minute)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	e := env()
	e.Vars = vars()
	e.Vars[source.VarHome] = filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	e.Vars[source.VarTogoHome] = t.TempDir()
	return e, first, second
}

func sessionStart(t *testing.T, h *hooks, src, p string) {
	t.Helper()
	id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
	h.Map(hookRecord(t, `{"hook_event_name":"SessionStart","source":"`+src+`","session_id":"`+id+`","transcript_path":"`+p+`"}`))
}

// Seen live: after a helper restart the chat came back empty when the
// session's folder held two transcripts (a /clear), because the path the
// SessionStart hook named lived only in memory and the folder guess gives
// up on two. The followed transcript is remembered under run/.
func TestRestartFollowsTheTranscriptAfterClear(t *testing.T) {
	e, first, second := followSetup(t)
	h := newHooks(e)
	sessionStart(t, h, "startup", first)
	sessionStart(t, h, "clear", second)
	if got := h.State()["transcript_path"]; got != second {
		t.Fatalf("before the restart: %q", got)
	}

	after := newHooks(e) // the helper restarted; the agent did not
	st := after.State()
	if st["transcript_path"] != second || st["harness_session"] != "7c1d2e3f-aaaa-4bbb-8ccc-ddddeeeeffff" {
		t.Fatalf("after the restart: transcript %q, session %q; want the second conversation", st["transcript_path"], st["harness_session"])
	}
	if st["transcript_dir"] != strings.TrimSuffix(second, ".jsonl") {
		t.Fatalf("transcript_dir %q", st["transcript_dir"])
	}
	saved := filepath.Join(e.Vars[source.VarTogoHome], "run", "follow-"+e.SessionID+"."+followName+".json")
	if fi, err := os.Stat(saved); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("remembered file: %v %v", fi, err)
	}

	// The removed session forgets it.
	source.RemoveFollows(e.Vars[source.VarTogoHome], e.SessionID)
	if got := newHooks(e).State()["transcript_path"]; got != "" {
		t.Fatalf("after removal the chat follows %q", got)
	}
}

// A remembered path is checked before it is followed: outside the
// agent's projects folder, a link, a folder or a missing file falls back
// to the folder guess.
func TestRememberedTranscriptIsValidated(t *testing.T) {
	e, first, second := followSetup(t)
	outside := filepath.Join(t.TempDir(), "x.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(first), "link.jsonl")
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(filepath.Dir(first), "dir.jsonl")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{outside, link, sub, filepath.Join(filepath.Dir(first), "gone.jsonl"),
		filepath.Join(filepath.Dir(first), "..", "..", "..", "x.jsonl"), "relative.jsonl", strings.TrimSuffix(first, ".jsonl") + ".txt"} {
		source.SaveFollowed(e, followName, bad)
		if got := newHooks(e).State()["transcript_path"]; got != "" {
			t.Errorf("remembered %q: followed %q, want the guess (none with two transcripts)", bad, got)
		}
	}
	// Gone, with one transcript left: the guess finds it.
	source.SaveFollowed(e, followName, filepath.Join(filepath.Dir(first), "gone.jsonl"))
	for _, p := range []string{first, link, sub} {
		_ = os.Remove(p)
	}
	if got := newHooks(e).State()["transcript_path"]; got != second {
		t.Errorf("stored transcript gone: followed %q, want the folder guess %q", got, second)
	}
}

// The agent exited before the restart: the chat still shows its last
// conversation, and the next Claude Code started in the pane is taken,
// not dropped as a nested run.
func TestRestartAfterExitTakesTheNextStart(t *testing.T) {
	e, first, second := followSetup(t)
	h := newHooks(e)
	sessionStart(t, h, "startup", first)
	h.Map(hookRecord(t, `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"0b6e5f8e-1111-4222-8333-944455556666"}`))

	after := newHooks(e)
	if got := after.State()["transcript_path"]; got != first {
		t.Fatalf("after the restart: %q, want the exited conversation", got)
	}
	sessionStart(t, after, "startup", second)
	if got := after.State()["transcript_path"]; got != second {
		t.Fatalf("the agent started again: followed %q, want %q", got, second)
	}
	// The new start is remembered and no longer ended: a nested run after
	// another restart is still dropped.
	again := newHooks(e)
	again.Map(hookRecord(t, `{"hook_event_name":"SessionStart","source":"startup","session_id":"nested","transcript_path":"`+first+`"}`))
	if got := again.State()["transcript_path"]; got != second {
		t.Fatalf("a nested run moved the chat to %q", got)
	}
}

// A relaunch on the same conversation (--resume) keeps it; one that
// starts a new conversation forgets the old one, so a restart before the
// new SessionStart does not load it.
func TestRelaunchUpdatesTheRememberedTranscript(t *testing.T) {
	e, first, second := followSetup(t)
	h := newHooks(e)
	sessionStart(t, h, "startup", first)
	h.Relaunched([]string{"claude", "--resume", "7c1d2e3f-aaaa-4bbb-8ccc-ddddeeeeffff", "--model", "opus"}, time.Now())
	if got := newHooks(e).State()["transcript_path"]; got != second {
		t.Fatalf("after a resume relaunch: %q, want %q", got, second)
	}
	h.Relaunched([]string{"claude", "--model", "opus"}, time.Now())
	if got := source.LoadFollowed(e, followName); got != "" {
		t.Fatalf("after a fresh relaunch the old transcript is remembered: %q", got)
	}
	h.Relaunched([]string{"claude", "--resume", "--dangerously-skip-permissions"}, time.Now())
	if got := source.LoadFollowed(e, followName); got != "" {
		t.Fatalf("a flag as the resume id was remembered: %q", got)
	}
}

// countProcs is a process view that counts n Claude Code processes in
// the session's tree.
type countProcs struct{ n int }

func (countProcs) Tree(context.Context) []int { return []int{1} }
func (countProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (countProcs) Others(context.Context, []string, string) (bool, error) { return false, nil }
func (c countProcs) Within(context.Context, []string) (int, error)        { return c.n, nil }

// The agent died with no SessionEnd (killed, or it exited while the
// helper was down) and the helper restarted. A Claude Code the user then
// starts in the pane is the session's own when it is the only one there;
// with the followed one still running below it, the start is a nested run.
func TestStartAfterAnUnendedDeath(t *testing.T) {
	e, first, second := followSetup(t)
	sessionStart(t, newHooks(e), "startup", first)
	for _, c := range []struct {
		running int
		want    string
	}{{2, first}, {1, second}} {
		e.Procs = countProcs{c.running}
		after := newHooks(e)
		sessionStart(t, after, "startup", second)
		if got := after.State()["transcript_path"]; got != c.want {
			t.Errorf("%d Claude Code running: followed %q, want %q", c.running, got, c.want)
		}
	}
}
