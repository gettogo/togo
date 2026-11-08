package goose

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "goose" {
		t.Fatalf("adapter %q", a.Adapter)
	}
	if a.Feeds["chat"].SQLite == nil || a.Feeds["session"].Mapper != MapperDB {
		t.Fatalf("feeds %+v", a.Feeds)
	}
}

func feed(t *testing.T, m *db, name string, final map[string]harness.Item) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(sc.Text())})
		for _, x := range items {
			final[x.ID] = x
		}
	}
}

// The fixtures are rows of a real Goose 1.57 sessions.db (paths scrubbed,
// long text cut): a plain reply, a shell call answered in chat mode, and a
// prompt whose reply never came.
func TestSessionRows(t *testing.T) {
	m := newDB(harness.Env{SessionID: "s_goose00001", Cwd: "/w/proj"})
	final := map[string]harness.Item{}
	feed(t, m, "sessions.jsonl", final)
	st := m.State()
	if st["harness_session"] != "20261108_4" || st["mode"] != "chat" {
		t.Fatalf("state %v", st)
	}
	feed(t, m, "messages.jsonl", final)
	if u := final["msg_79033e89-7946-4773-b33e-fcc3dfc53db5"].User; u == nil || u.Text != "Reply with only the word ok." {
		t.Fatalf("user %+v", u)
	}
	for _, x := range final {
		if x.User != nil && len(x.User.Text) > 0 && x.User.Text[0] == '<' {
			t.Fatalf("turn context shown as a prompt: %q", x.User.Text)
		}
	}
	if r := final["gen-1794101377-Ao4VcIGwMR3FVpGcMDjS:1"].Assistant; r == nil || r.Text != "ok" {
		t.Fatalf("reply %+v", r)
	}
	if final["gen-1794101377-Ao4VcIGwMR3FVpGcMDjS:0"].Thinking == nil {
		t.Fatal("thinking missing")
	}
	if tb := final["msg_79033e89-7946-4773-b33e-fcc3dfc53db5.turn"].Turn; tb == nil || tb.State != harness.StateOK {
		t.Fatalf("first turn %+v", tb)
	}
	c := final["call_6501416ccae64397930ec184"].Tool
	if c == nil || c.Name != "shell" || c.State != harness.StateOK || c.Output == "" {
		t.Fatalf("call %+v", c)
	}
	if tb := final["msg_9d9fc175-9e91-4f72-9ad1-6d3083255859.turn"].Turn; tb == nil || tb.State != harness.StateOK || tb.Steps != 1 {
		t.Fatalf("tool turn %+v", tb)
	}
	if tb := final["msg_5d7e70d3-0e4e-46fc-aa4c-78e7f63df2d6.turn"].Turn; tb == nil || tb.State != harness.StateRunning {
		t.Fatalf("open turn %+v", tb)
	}
	if m.State()["working"] != "true" {
		t.Fatal("not working during the open turn")
	}
}

func TestOtherFolderIgnored(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/none"})
	feed(t, m, "sessions.jsonl", map[string]harness.Item{})
	if s := m.State()["harness_session"]; s != "" {
		t.Fatalf("picked %q", s)
	}
	final := map[string]harness.Item{}
	feed(t, m, "messages.jsonl", final)
	if len(final) != 0 {
		t.Fatalf("%d items from another session", len(final))
	}
}

// errors.jsonl is a real Goose 1.57 session (paths and host scrubbed) with
// two failed turns: the provider unreachable, then an unknown model (404).
// Each error part is an error notice and ends its turn failed.
func TestErrorParts(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	final := map[string]harness.Item{}
	feed(t, m, "errors.jsonl", final)
	cases := []struct{ id, reason, text string }{
		{"msg_dc88eaec-7329-49c9-89b7-28905a51b483:0", harness.ErrProviderUnreachable,
			"Can't reach the model provider. Network error: Could not connect to 127.0.0.1:9 — check your network connection and try again"},
		{"msg_20d10145-1639-4fe8-9bb8-e3cdd5da190c:0", harness.ErrModelNotFound,
			"The provider doesn't have this model. Request failed: Resource not found (404) at http://gpu.example:8080/v1/chat/completions: no router for requested model. Available models for this provider: qwen3.8-27b"},
	}
	for _, c := range cases {
		n := final[c.id].Notice
		if n == nil || n.Code != harness.NoticeError || n.Level != "error" || n.Reason != c.reason || n.Text != c.text {
			t.Fatalf("%s: %+v", c.id, n)
		}
	}
	for _, id := range []string{"msg_cde51fa1-556a-409a-9465-3755e6e1ef21.turn", "msg_7fbf9e61-2402-49ae-a085-16e626b0de1f.turn"} {
		if tb := final[id].Turn; tb == nil || tb.State != harness.StateFailed || tb.Ended == nil {
			t.Fatalf("%s: %+v", id, tb)
		}
	}
	if m.State()["working"] == "true" {
		t.Fatal("still working after the error")
	}
}

func TestErrorKinds(t *testing.T) {
	for _, c := range []struct{ kind, msg, reason string }{
		{"authentication", "Authentication failed: 401 invalid key", harness.ErrAuth},
		{"contextLengthExceeded", "Context length exceeded", harness.ErrContextOverflow},
		{"other", "Ran into this error: Rate limit exceeded: 429 Too Many Requests.\n\nPlease retry if you think this is a transient or recoverable error.", harness.ErrRateLimit},
	} {
		if r, _ := errorReason(c.kind, c.msg); r != c.reason {
			t.Fatalf("%s %q: %s", c.kind, c.msg, r)
		}
	}
}
