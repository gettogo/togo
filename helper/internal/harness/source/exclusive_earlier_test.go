package source

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pi 0.73 resumes a conversation at the desk by reading its file
// (SessionManager.setSessionFile: readFileSync, no write and no open file
// kept), so nothing tells the session before the next prompt. The prompt
// appends to that file: the session then moves there and reads it from
// its start, so the chat shows the conversation's earlier entries before
// the new one. The file is one the session never followed, written long
// before the session began.
func TestExclusiveUnheldResumeOfAnEarlierConversation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "adapters", "pisession", "testdata", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	env, h := testEnv(t)
	env.SessionID, env.Agent = "s_earlier001", "pi"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	env.Vars["togo_home"] = filepath.Join(h, ".togo")
	env.Procs = &fakeProcs{tree: []int{100}}
	earlier := filepath.Join(h, stamped(time.Now().Add(-time.Hour), "earlier"))
	current := filepath.Join(h, stamped(time.Now().Add(-55*time.Second), "current"))
	// Its second prompt (line 14) is the one typed after the resume.
	const at = 13
	if !bytes.Contains(lines[at], []byte(`"role":"user"`)) {
		t.Fatal("fixture: line 14 is not a prompt")
	}
	for _, l := range lines[:at] {
		writeLine(t, earlier, string(l))
	}
	touch(t, earlier, time.Now().Add(-time.Hour))
	writeLine(t, current, `{"type":"session","version":3,"id":"0d9f6c1e-0000-4000-8000-00000000c0de","timestamp":"2026-11-06T15:00:00.000Z","cwd":"/Users/user/proj"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)

	// The prompt typed after /resume of the earlier conversation.
	prompt := string(lines[at])
	writeLine(t, earlier, prompt)
	got := c.wait(t, 2+at)
	if got[1] != string(lines[0]) || got[len(got)-1] != prompt {
		t.Fatalf("the earlier conversation was not read from its start: first %q, last %q", got[1], got[len(got)-1])
	}
	if loadFollow(env) != earlier {
		t.Fatalf("remembered %q, want the resumed file", loadFollow(env))
	}
}
