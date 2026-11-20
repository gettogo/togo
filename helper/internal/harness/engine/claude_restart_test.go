package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// claudeEngine serves one Claude Code session over a fake `claude` that
// only prints its version.
func claudeEngine(t *testing.T, id, home, togoHome string, created time.Time) *Engine {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo '2.1.319 (Claude Code)';;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pane := &fakePane{mode: "a"}
	return New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "claude", Managed: true, Cwd: filepath.Join(home, "proj"), CreatedAt: created}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home, TogoHome: togoHome,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.2,
	})
}

// claudeTranscript writes a real transcript fixture as one conversation
// of the session's folder.
func claudeTranscript(t *testing.T, home, fixture, conv string, at time.Time) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "adapters", "claude", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".claude", "projects", CwdKey(filepath.Join(home, "proj")))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, conv+".jsonl")
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(raw), "/HOME", home)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	return p
}

func claudeHook(id string, v map[string]any) {
	b, _ := json.Marshal(v)
	source.Hooks.Deliver(id, source.Payload{Kind: source.BusHook, Event: v["hook_event_name"].(string), Data: b})
}

// Seen live: `togo update` restarted the helper under a Claude Code
// session that had run /clear, and the chat came back empty until the
// next hook. The session follows the second of two transcripts in its
// folder; after the restart the chat shows that conversation again, the
// same items, without starting over and without a prompt twice.
func TestClaudeChatSurvivesRestartAfterClear(t *testing.T) {
	const id = "s_claudefol1"
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	togoHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "proj"), 0o700); err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-time.Hour).Truncate(time.Second)
	first := claudeTranscript(t, home, "transcript-basic.jsonl", "0b6e5f8e-1111-4222-8333-944455556666", created.Add(time.Minute))
	second := claudeTranscript(t, home, "transcript-real.jsonl", "ca4d237e-2b33-48c7-914c-8a8a6f490d08", created.Add(2*time.Minute))
	ctx := context.Background()

	e1 := claudeEngine(t, id, home, togoHome, created)
	if _, err := e1.served(ctx, id); err != nil {
		t.Fatal(err)
	}
	claudeHook(id, map[string]any{"hook_event_name": "SessionStart", "source": "startup", "session_id": "0b6e5f8e-1111-4222-8333-944455556666", "transcript_path": first})
	waitFor(t, func() bool { _, ok := e1.log.Get(id, "u1.turn"); return ok })
	claudeHook(id, map[string]any{"hook_event_name": "SessionEnd", "reason": "clear", "session_id": "0b6e5f8e-1111-4222-8333-944455556666"})
	claudeHook(id, map[string]any{"hook_event_name": "SessionStart", "source": "clear", "session_id": "ca4d237e-2b33-48c7-914c-8a8a6f490d08", "transcript_path": second})
	// The chat started over on the second conversation and read it to its
	// last record (the plan) before the helper stops.
	const last = "plan-f0992ad3-2182-4dea-a38f-7b2b5a77b1be"
	waitFor(t, func() bool {
		_, old := e1.log.Get(id, "u1.turn")
		_, done := e1.log.Get(id, last)
		return done && !old
	})
	items, _, _ := e1.log.Since(id, 0, 0)
	before := itemView(items)
	e1.Unload(id)

	e2 := claudeEngine(t, id, home, togoHome, created)
	t.Cleanup(func() { e2.Forget(id) })
	var resets atomic.Int32
	sub := &fakeSub{done: make(chan struct{})}
	if _, err := e2.Open(ctx, sub, id, 0, 500); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var after []string
	for {
		_, done := e2.log.Get(id, last)
		items, _, _ := e2.log.Since(id, 0, 0)
		after = itemView(items)
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("after the restart the chat holds:\n%s\nbefore:\n%s", strings.Join(after, "\n"), strings.Join(before, "\n"))
	}
	sub.mu.Lock()
	for _, n := range sub.got {
		if n.Kind == harness.NoteReset {
			resets.Add(1)
		}
	}
	sub.mu.Unlock()
	if resets.Load() != 0 {
		t.Errorf("the chat started over %d times after the restart", resets.Load())
	}
	users := 0
	for _, v := range after {
		if strings.Contains(v, "|"+harness.KindUser+"|") {
			users++
		}
	}
	wantUsers := 0
	for _, v := range before {
		if strings.Contains(v, "|"+harness.KindUser+"|") {
			wantUsers++
		}
	}
	if users != wantUsers {
		t.Errorf("%d prompts after the restart, %d before", users, wantUsers)
	}
}
