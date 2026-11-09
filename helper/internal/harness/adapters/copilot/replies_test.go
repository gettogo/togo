package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

const testSession = "be487eb1-810c-4f02-86b6-ccd46306c9d7"

// Lines shaped as Copilot CLI 1.0.107 writes them to
// ~/.copilot/session-state/<id>/events.jsonl (checked live).
const (
	evUser    = `{"type":"user.message","data":{"content":"Add a header","transformedContent":"Add a header","source":"user","interactionId":"i1"},"id":"e1","timestamp":"2026-11-09T03:05:30.345Z","parentId":"e0"}`
	evToolMsg = `{"type":"assistant.message","data":{"messageId":"m1","content":"","toolRequests":[{"toolCallId":"toolu_1","name":"bash","arguments":{"command":"cat src/app.ts"}}],"turnId":"0"},"id":"e2","timestamp":"2026-11-09T03:05:31.827Z","parentId":"e1"}`
	evNarrate = `{"type":"assistant.message","data":{"messageId":"m2","content":"Middleware goes first.","toolRequests":[{"toolCallId":"toolu_2","name":"bash","arguments":{"command":"npm test"}}],"turnId":"1"},"id":"e3","timestamp":"2026-11-09T03:05:36.842Z","parentId":"e2"}`
	evFinal   = `{"type":"assistant.message","data":{"messageId":"m3","content":"Every response now carries an X-Request-Id header.","toolRequests":[],"turnId":"2"},"id":"e4","timestamp":"2026-11-09T03:05:39.994Z","parentId":"e3"}`
	evEnd     = `{"type":"assistant.turn_end","data":{"turnId":"2"},"id":"e5","timestamp":"2026-11-09T03:05:39.995Z","parentId":"e4"}`
)

func eventsFile(t *testing.T) (harness.Env, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", testSession)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return harness.Env{SessionID: "s_copilot001", Cwd: "/tmp/p", Vars: map[string]string{source.VarHome: home}}, filepath.Join(dir, "events.jsonl")
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepliesReadsNewAgentMessages(t *testing.T) {
	env, path := eventsFile(t)
	appendLines(t, path, evUser)
	got, end := replies(env, testSession, -1)
	if len(got) != 0 || end <= 0 {
		t.Fatalf("from the end: %q, %d", got, end)
	}
	appendLines(t, path, evToolMsg, evNarrate, evFinal, evEnd)
	got, next := replies(env, testSession, end)
	if len(got) != 3 || got[0].Text != "" || got[0].Calls[0] != "toolu_1" ||
		got[1].Text != "Middleware goes first." || got[1].Calls[0] != "toolu_2" ||
		got[2].Text != "Every response now carries an X-Request-Id header." || len(got[2].Calls) != 0 || next <= end {
		t.Fatalf("got %+v, next %d", got, next)
	}
	if again, n := replies(env, testSession, next); len(again) != 0 || n != next {
		t.Fatalf("read twice: %q, %d", again, n)
	}
	// A line still being written waits for its newline.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(evFinal[:40])
	f.Close()
	if part, n := replies(env, testSession, next); len(part) != 0 || n != next {
		t.Fatalf("partial line: %q, %d", part, n)
	}
}

func TestRepliesRefusesOtherPaths(t *testing.T) {
	env, path := eventsFile(t)
	appendLines(t, path, evFinal)
	for _, id := range []string{"", "../" + testSession, testSession + "/..", "a/b"} {
		if got, n := replies(env, id, 0); got != nil || n != 0 {
			t.Errorf("%q: %q, %d", id, got, n)
		}
	}
}

func hook(t *testing.T, m harness.Mapper, at time.Time, p map[string]any) []harness.Item {
	t.Helper()
	raw, _ := json.Marshal(p)
	items, _ := m.Map(harness.Record{Source: "hooks", At: at, Data: raw})
	return items
}

// Copilot's hooks carry no reply text, so the chat showed its tools but
// never what it said. Its messages come from its own session log as hooks
// arrive: narration before the tool it precedes, the summary before the
// turn ends.
func TestCopilotChatShowsWhatItSaid(t *testing.T) {
	env, path := eventsFile(t)
	newMapper, ok := harness.LookupMapper(MapperHooks)
	if !ok {
		t.Fatal("mapper not registered")
	}
	m := newMapper(env)
	at := time.Date(2026, 11, 9, 3, 5, 30, 0, time.UTC)
	appendLines(t, path, `{"type":"session.start","data":{}}`)
	hook(t, m, at, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": testSession, "prompt": "Add a header"})
	appendLines(t, path, evUser, evToolMsg)
	go func() {
		time.Sleep(150 * time.Millisecond)
		appendLines(t, path, evNarrate)
	}()
	items := hook(t, m, at.Add(6*time.Second), map[string]any{"hook_event_name": "PreToolUse", "session_id": testSession,
		"tool_name": "bash", "tool_use_id": "toolu_2", "tool_input": map[string]any{"command": "npm test"}})
	if len(items) < 2 || items[0].Kind != harness.KindAssistant || items[0].Assistant.Text != "Middleware goes first." || items[1].Kind != harness.KindTool {
		t.Fatalf("pre tool: %+v", kinds(items))
	}
	// Copilot writes its log a moment after the hook fires: the read waits
	// for the reply the hook implies.
	go func() {
		time.Sleep(150 * time.Millisecond)
		appendLines(t, path, evFinal, evEnd)
	}()
	items = hook(t, m, at.Add(10*time.Second), map[string]any{"hook_event_name": "Stop", "session_id": testSession})
	if len(items) < 2 || items[0].Kind != harness.KindAssistant || items[0].Assistant.Text != "Every response now carries an X-Request-Id header." {
		t.Fatalf("stop: %+v", kinds(items))
	}
	if last := items[len(items)-1]; last.Kind != harness.KindTurn || items[0].TurnID != last.TurnID {
		t.Fatalf("stop: %+v", kinds(items))
	}
}

func kinds(items []harness.Item) []string {
	var out []string
	for _, x := range items {
		s := x.Kind
		if x.Assistant != nil {
			s += ":" + x.Assistant.Text
		}
		out = append(out, s)
	}
	return out
}
