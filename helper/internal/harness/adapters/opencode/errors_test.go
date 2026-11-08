package opencode

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// jsonl reads one event per line, as the sse runner hands them over.
func jsonl(t *testing.T, name string) []harness.Record {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	var out []harness.Record
	for i := 0; sc.Scan(); i++ {
		out = append(out, harness.Record{Source: "events", At: t0.Add(time.Duration(i) * time.Second),
			Data: json.RawMessage(append([]byte(nil), sc.Bytes()...))})
	}
	return out
}

func notices(items map[string]harness.Item) []harness.Item {
	var out []harness.Item
	for _, x := range items {
		if x.Notice != nil {
			out = append(out, x)
		}
	}
	return out
}

// OpenCode 2.2.5 asked a llama-swap server for a model it does not serve:
// step.failed and execution.failed carry the same 404; the chat shows it
// once and the turn ends failed.
func TestV2ModelNotFound(t *testing.T) {
	m := NewEvents2(harness.Env{SessionID: "s", Cwd: "/w/proj"})
	got := run(m, jsonl(t, "testdata/v2/model-not-found.jsonl")).last()
	ns := notices(got)
	if len(ns) != 1 {
		t.Fatalf("notices %+v", ns)
	}
	n := ns[0].Notice
	if n.Reason != harness.ErrModelNotFound || n.Level != "error" || n.Text != "The provider doesn't have this model: no router for requested model" {
		t.Fatalf("notice %+v", n)
	}
	for _, x := range got {
		if x.Turn != nil && (x.Turn.State != harness.StateFailed || x.Turn.Ended == nil) {
			t.Fatalf("turn %+v", x.Turn)
		}
	}
	if m.State()["working"] == "true" {
		t.Fatal("still working")
	}
}

// OpenCode 2.2.5 against a closed port retries the call; the chat shows
// one notice that the provider is unreachable, updated per attempt, and
// the interrupt ends the turn.
func TestV2RetryTransport(t *testing.T) {
	m := NewEvents2(harness.Env{SessionID: "s", Cwd: "/w/proj"})
	got := run(m, jsonl(t, "testdata/v2/retry-transport.jsonl")).last()
	var retry *harness.NoticeBody
	for _, x := range notices(got) {
		if x.Notice.Code != harness.NoticeInterrupted {
			if retry != nil {
				t.Fatalf("second retry notice %+v", x.Notice)
			}
			retry = x.Notice
		}
	}
	if retry == nil || retry.Reason != harness.ErrProviderUnreachable || retry.Code != harness.NoticeError ||
		!strings.HasPrefix(retry.Text, "Can't reach the provider: ECONNREFUSED") || !strings.HasSuffix(retry.Text, "(retrying, attempt 8)") {
		t.Fatalf("retry %+v", retry)
	}
	if m.State()["working"] == "true" {
		t.Fatal("still working")
	}
}

// Kilo 7.8.27 against a closed port reports the session offline; the
// chat says the provider is unreachable until the interrupt.
func TestKiloOffline(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s", Cwd: "/w/proj"})
	got := run(m, jsonl(t, "testdata/kilo/offline.jsonl")).last()
	var off *harness.NoticeBody
	for _, x := range notices(got) {
		if x.Notice.Reason != "" {
			if off != nil {
				t.Fatalf("second error notice %+v", x.Notice)
			}
			off = x.Notice
		}
	}
	if off == nil || off.Reason != harness.ErrProviderUnreachable || off.Text != "Can't reach the provider: Connection refused (retrying)" {
		t.Fatalf("offline %+v", off)
	}
	if m.State()["working"] == "true" {
		t.Fatal("still working")
	}
}

// OpenCode 1.x and Kilo errors by name and status (message.error shapes
// from the 1.x SDK types).
func TestV1ErrorNames(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		msg    string
		reason string
		text   string
	}{
		{"ProviderAuthError", 0, "Invalid API key", harness.ErrAuth, "The provider refused the API key: Invalid API key"},
		{"APIError", 401, "Unauthorized", harness.ErrAuth, "The provider refused the API key: Unauthorized"},
		{"APIError", 429, "Too Many Requests", harness.ErrRateLimit, "The provider's rate limit or quota was hit: Too Many Requests"},
		{"APIError", 404, "no router for requested model", harness.ErrModelNotFound, "The provider doesn't have this model: no router for requested model"},
		{"ContextOverflowError", 0, "", harness.ErrContextOverflow, "The conversation is too long for the model."},
		{"MessageOutputLengthError", 0, "", harness.ErrOther, "The reply hit the model's output limit."},
		{"UnknownError", 0, "boom", harness.ErrOther, "boom"},
	} {
		b := errorBody(c.name, c.status, c.msg)
		if b.Reason != c.reason || b.Text != c.text {
			t.Errorf("%s %d: %+v", c.name, c.status, b)
		}
	}
}

// A failed message and the session.error that follows it are one notice,
// and the turn ends failed.
func TestV1ErrorOnce(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s", Cwd: "/w/proj"})
	ev := func(typ, props string) harness.Record {
		return harness.Record{At: t0, Data: json.RawMessage(`{"type":"` + typ + `","properties":` + props + `}`)}
	}
	recs := []harness.Record{
		ev("session.updated", `{"info":{"id":"ses_a","directory":"/w/proj","time":{"created":1,"updated":2}}}`),
		ev("message.updated", `{"info":{"id":"msg_u","sessionID":"ses_a","role":"user","time":{"created":1}}}`),
		ev("session.status", `{"sessionID":"ses_a","status":{"type":"busy"}}`),
		ev("message.updated", `{"info":{"id":"msg_a","sessionID":"ses_a","role":"assistant","parentID":"msg_u","time":{"created":2},"error":{"name":"ProviderAuthError","data":{"providerID":"x","message":"Invalid API key"}}}}`),
		ev("session.error", `{"sessionID":"ses_a","error":{"name":"ProviderAuthError","data":{"providerID":"x","message":"Invalid API key"}}}`),
		ev("session.idle", `{"sessionID":"ses_a"}`),
	}
	got := run(m, recs).last()
	ns := notices(got)
	if len(ns) != 1 || ns[0].Notice.Reason != harness.ErrAuth {
		t.Fatalf("notices %+v", ns)
	}
	if tu := got["msg_u"].Turn; tu == nil || tu.State != harness.StateFailed || tu.Ended == nil {
		t.Fatalf("turn %+v", got["msg_u"])
	}
}
