package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

const (
	threadA = "019a0000-0000-7000-8000-000000000001" // the recorded thread
	threadB = "019a0000-0000-7000-8000-0000000000bb"
)

// resumeOf is the recorded thread's resume result as another top-level
// thread, with its turn started at turnAt and its own item ids.
func resumeOf(t *testing.T, thread string, turnAt int64) harness.Record {
	t.Helper()
	raw := string(records(t, "appserver-turn.jsonl")[0].Data)
	if thread != threadA {
		raw = strings.ReplaceAll(raw, threadA, thread)
		for _, id := range []string{"u-old", "call_old", "msg_old", "019a0000-0000-7000-8000-0000000000a1"} {
			raw = strings.ReplaceAll(raw, `"`+id+`"`, `"`+id+`-b"`)
		}
	}
	raw = strings.Replace(raw, `"startedAt": 1794000000`, `"startedAt": `+itoa(int(turnAt)), 1)
	return harness.Record{At: t0, Data: json.RawMessage(raw)}
}

func itemIDs(items []harness.Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return strings.Join(out, " ")
}

// Seen live: after a helper restart the chat showed the resumed
// conversation followed by the /new one. The server lists both top-level
// threads and the feed joins each; the history of the one that did not
// become the main thread went into the chat too.
func TestOtherTopLevelThreadHistoryStaysOut(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_codexmix1"})
	m.Map(resumeOf(t, threadB, 1794000100)) // the newer one, joined first
	items, _ := m.Map(resumeOf(t, threadA, 1794000000))
	if got := m.State()["harness_session"]; got != threadB {
		t.Fatalf("main thread %q, want %q", got, threadB)
	}
	if len(items) != 0 {
		t.Fatalf("the other thread's history went into the chat: %s", itemIDs(items))
	}
}

// The thread the chat followed is remembered for a helper restart: when
// the server lists it again it stays the main thread, though another
// top-level thread listed with it saw more recent activity.
func TestRestartKeepsTheFollowedThread(t *testing.T) {
	env := harness.Env{SessionID: "s_codexfol1", Vars: map[string]string{source.VarTogoHome: t.TempDir()}}
	// The chat follows thread A (the desk resumed it after a /new left
	// thread B loaded).
	m := NewAppServer(env)
	m.Map(resumeOf(t, threadA, 1794000000))
	if got := m.State()["harness_session"]; got != threadA {
		t.Fatalf("before the restart the main thread is %q", got)
	}

	for _, order := range [][]string{{threadA, threadB}, {threadB, threadA}} {
		after := NewAppServer(env)
		var shown []harness.Item
		for _, id := range order {
			at := int64(1794000000)
			if id == threadB {
				at = 1794000100
			}
			items, _ := after.Map(resumeOf(t, id, at))
			if after.State()["harness_session"] == threadA {
				shown = append(shown, items...)
			}
		}
		if got := after.State()["harness_session"]; got != threadA {
			t.Fatalf("joined %v after the restart: main thread %q, want the followed %q", order, got, threadA)
		}
		if ids := itemIDs(shown); strings.Contains(ids, "-b") {
			t.Fatalf("joined %v: the other thread's items %s", order, ids)
		}
	}

	source.RemoveFollows(env.Vars[source.VarTogoHome], env.SessionID)
	after := NewAppServer(env)
	after.Map(resumeOf(t, threadA, 1794000000))
	after.Map(resumeOf(t, threadB, 1794000100))
	if got := after.State()["harness_session"]; got != threadB {
		t.Fatalf("with nothing remembered the most recent thread is main, got %q", got)
	}
}
