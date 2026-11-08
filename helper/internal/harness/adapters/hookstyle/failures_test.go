package hookstyle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// feed normalizes hook calls for agent and maps them, returning every item.
func feed(t *testing.T, agent string, calls ...[2]string) ([]harness.Item, *Mapper) {
	t.Helper()
	d, _ := For(agent)
	m := New(harness.Env{SessionID: "s-" + agent + "-" + t.Name()}, Options{Name: agent + ".hooks"})
	var out []harness.Item
	for i, c := range calls {
		_, norm, err := Normalize(d, c[0], []byte(c[1]))
		if err != nil {
			t.Fatal(err)
		}
		items, _ := m.Map(harness.Record{Source: "hooks", At: t0.Add(time.Duration(i) * time.Second), Data: norm})
		out = append(out, items...)
	}
	return out, m
}

func failed(items []harness.Item) (turn string, notice *harness.NoticeBody) {
	for _, it := range items {
		if it.Turn != nil && it.Kind == harness.KindTurn && it.Turn.Ended != nil {
			turn = it.Turn.State
		}
		if it.Notice != nil {
			notice = it.Notice
		}
	}
	return
}

// A real Droid 1.5 transcript of a turn whose provider was unreachable:
// the Stop hook looks like success, the transcript says otherwise.
func TestDroidStopReadsTurnOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".factory", "sessions", "-HOME-proj", "s.jsonl")
	b, err := os.ReadFile("testdata/droid-transcript-unreachable.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, b, 0o600)
	stop, _ := json.Marshal(map[string]any{"session_id": "d1", "transcript_path": path, "hook_event_name": "Stop"})
	items, m := feed(t, "droid",
		[2]string{"UserPromptSubmit", `{"session_id":"d1","prompt":"hi"}`},
		[2]string{"Stop", string(stop)})
	state, n := failed(items)
	if state != harness.StateFailed {
		t.Fatalf("turn %q", state)
	}
	if n == nil || n.Reason != harness.ErrProviderUnreachable || !strings.HasPrefix(n.Text, "Could not reach the provider: Unable to reach http://127.0.0.1:9.") {
		t.Fatalf("notice %+v", n)
	}
	if m.State()["working"] != "false" {
		t.Fatal("still working")
	}
}

func TestDroidIgnoresOtherPaths(t *testing.T) {
	if _, _, ok := droidOutcome("/etc/../tmp/x.jsonl"); ok {
		t.Fatal("read a path outside Droid's sessions")
	}
}

func TestDroidCompletedStopIsOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".factory", "sessions", "p", "s.jsonl")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}
{"type":"agent_turn_outcome","turnId":"x","reason":"completed","resultKind":"text"}
`), 0o600)
	stop, _ := json.Marshal(map[string]any{"session_id": "d1", "transcript_path": path})
	items, _ := feed(t, "droid", [2]string{"UserPromptSubmit", `{"session_id":"d1","prompt":"hi"}`}, [2]string{"Stop", string(stop)})
	if state, n := failed(items); state != harness.StateOK || n != nil {
		t.Fatalf("turn %q notice %+v", state, n)
	}
}

// Droid fires no Stop on an abort, only this Notification.
func TestDroidAbortNotificationEndsTurn(t *testing.T) {
	items, m := feed(t, "droid",
		[2]string{"UserPromptSubmit", `{"session_id":"d1","prompt":"hi"}`},
		[2]string{"Notification", `{"session_id":"d1","message":"Agent stopped by user and is waiting for input","notification_type":"idle_prompt"}`})
	if state, _ := failed(items); state != harness.StateInterrupted || m.State()["working"] != "false" {
		t.Fatalf("turn %q state %v", state, m.State())
	}
}

func TestCopilotFatalErrorEndsTurn(t *testing.T) {
	items, m := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{"prompt":"hi"}`},
		[2]string{"errorOccurred", `{"error":{"message":"Request failed with status 401: Unauthorized","name":"CAPIError"},"errorContext":"model_call","recoverable":false}`})
	state, n := failed(items)
	if state != harness.StateFailed || n == nil || n.Reason != harness.ErrAuth || n.Level != "error" ||
		n.Text != "The provider rejected the sign-in or API key: Request failed with status 401: Unauthorized" {
		t.Fatalf("turn %q notice %+v", state, n)
	}
	if m.State()["working"] != "false" {
		t.Fatal("still working")
	}
}

func TestCopilotSessionEndReasons(t *testing.T) {
	items, _ := feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{"prompt":"hi"}`},
		[2]string{"sessionEnd", `{"reason":"error","error":"429 quota exceeded"}`})
	if state, n := failed(items); state != harness.StateFailed || n == nil || n.Reason != harness.ErrRateLimit {
		t.Fatalf("error: turn %q notice %+v", state, n)
	}
	items, _ = feed(t, "copilot",
		[2]string{"userPromptSubmitted", `{"prompt":"hi"}`},
		[2]string{"sessionEnd", `{"reason":"abort"}`})
	if state, _ := failed(items); state != harness.StateInterrupted {
		t.Fatalf("abort: turn %q", state)
	}
}

// Cursor and Amp say a turn failed but not why.
func TestStatusOnlyFailuresSayToLookAtTerminal(t *testing.T) {
	for agent, calls := range map[string][][2]string{
		"cursor": {{"beforeSubmitPrompt", `{"conversation_id":"c1","prompt":"hi"}`}, {"stop", `{"conversation_id":"c1","status":"error"}`}},
		"amp":    {{"agent.start", `{"thread":{"id":"T-1"},"message":"hi"}`}, {"agent.end", `{"thread":{"id":"T-1"},"status":"error"}`}},
	} {
		items, _ := feed(t, agent, calls...)
		state, n := failed(items)
		if state != harness.StateFailed || n == nil || n.Code != harness.NoticeError || !strings.Contains(n.Text, "terminal") {
			t.Errorf("%s: turn %q notice %+v", agent, state, n)
		}
	}
}
