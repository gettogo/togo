package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pi and Kimi Code append and close their file per entry, so nobody holds
// it. After a /new the session follows the new file; a resume of the first
// conversation then appends to the file the session left. With no other
// agent of the kind in the folder that growth is this session's resume:
// the session moves back there, and to the newer file again when that one
// grows.
func TestExclusiveUnheldResumeToDroppedFile(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID, env.Agent = "s_resume0001", "pi"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	env.Vars["togo_home"] = filepath.Join(h, ".togo")
	env.Procs = &fakeProcs{tree: []int{100}}
	first, second := filepath.Join(h, stamped(time.Now().Add(-55*time.Second), "first")), filepath.Join(h, "second.jsonl")
	writeLine(t, first, `{"f":"a1"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	writeLine(t, second, `{"f":"b1"}`) // the /new
	c.wait(t, 2)
	time.Sleep(2 * droppedGrace())
	writeLine(t, first, `{"f":"a2"}`) // the resume
	// The file is taken from its start again: a1 a2 after a1 b1.
	got := c.wait(t, 4)
	if got[2] != `{"f":"a1"}` || got[3] != `{"f":"a2"}` {
		t.Fatalf("resume not followed: %v", got)
	}
	if loadFollow(env) != first {
		t.Fatalf("remembered %q, want the resumed file", loadFollow(env))
	}
	time.Sleep(2 * droppedGrace())
	writeLine(t, second, `{"f":"b2"}`) // resumed back to the second
	got = c.wait(t, 6)
	if got[4] != `{"f":"b1"}` || got[5] != `{"f":"b2"}` {
		t.Fatalf("second resume not followed: %v", got)
	}
}

// With another agent of the kind in the folder a growing file the session
// left may be the other agent's: it is not taken back (rule: two sessions
// in one folder never share a guess).
func TestExclusiveUnheldDroppedFileWithForeignAgentStays(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID, env.Agent = "s_resume0002", "pi"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	procs := &fakeProcs{tree: []int{100}}
	env.Procs = procs
	first, second := filepath.Join(h, stamped(time.Now().Add(-55*time.Second), "first")), filepath.Join(h, "second.jsonl")
	writeLine(t, first, `{"f":"a1"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	writeLine(t, second, `{"f":"b1"}`)
	c.wait(t, 2)
	procs.mu.Lock()
	procs.others = map[string]bool{env.Cwd: true}
	procs.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	writeLine(t, first, `{"f":"a2"}`)
	time.Sleep(400 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 2 {
		t.Fatalf("took back a file another agent may write: %v", c.recs)
	}
}

// Right after a /new the agent may still write to the conversation it
// left, and a sync tool may touch it: neither takes the chat back. A
// late write within the grace, seen at any time, is absorbed; a touch
// that adds no whole record is not a resume.
func TestExclusiveLateWriteToTheLeftFileStays(t *testing.T) {
	env, h := testEnv(t)
	env.SessionID, env.Agent = "s_resume0003", "pi"
	env.Vars["created"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	env.Procs = &fakeProcs{tree: []int{100}}
	first, second := filepath.Join(h, stamped(time.Now().Add(-55*time.Second), "first")), filepath.Join(h, "second.jsonl")
	writeLine(t, first, `{"f":"a1"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() { _ = File{kind: "jsonl"}.Watch(ctx, exclusiveSpec(), env, c.emit) }()
	c.wait(t, 1)
	time.Sleep(60 * time.Millisecond)
	writeLine(t, second, `{"f":"b1"}`) // the /new
	c.wait(t, 2)
	writeLine(t, first, `{"f":"a-late"}`) // the agent's last write to the left file
	time.Sleep(4 * droppedGrace())
	touch(t, first, time.Now()) // a sync tool
	time.Sleep(4 * globEvery)
	f, err := os.OpenFile(first, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"f":"half`) // a record not yet whole
	f.Close()
	time.Sleep(4 * globEvery)
	writeLine(t, second, `{"f":"b2"}`)
	got := c.wait(t, 3)
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) != 3 || got[2] != `{"f":"b2"}` {
		t.Fatalf("the left file took the chat back: %v", c.recs)
	}
}
