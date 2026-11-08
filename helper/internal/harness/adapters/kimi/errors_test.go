package kimi

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A real Kimi Code wire log whose stored login was rejected: the turn
// fails and the notice asks for a fresh sign-in.
func TestRejectedLoginFailsTurnWithNotice(t *testing.T) {
	w := NewWire(harness.Env{})
	got := replay(t, w, fixture(t, "wire-auth-failed.jsonl"))
	n := got.items["error-1794079438374"]
	if n.Notice == nil || n.Notice.Level != "error" || n.Notice.Reason != harness.ErrAuth ||
		!strings.HasPrefix(n.Notice.Text, "Kimi needs you to sign in again (run /login): Stored token for") {
		t.Fatalf("notice %+v", n)
	}
	if n.TurnID == "" {
		t.Fatal("notice outside the turn")
	}
	if tu := got.items[n.TurnID]; tu.Turn == nil || tu.Turn.State != harness.StateFailed {
		t.Fatalf("turn %+v", tu)
	}
	if w.State()["working"] != "false" {
		t.Fatal("still working")
	}
}

func TestFailureCodes(t *testing.T) {
	for line, want := range map[string]string{
		`{"type":"turn.ended","reason":"failed","error":{"code":"provider.rate_limit","message":"429 Too Many Requests"}}`:                 "Rate limited or out of credits: 429 Too Many Requests",
		`{"type":"turn.ended","reason":"failed","error":{"code":"provider.connection_error","message":"Connection error."}}`:               "Could not reach the provider: Connection error.",
		`{"type":"turn.ended","reason":"failed","error":{"code":"context.overflow","message":"too long"}}`:                                 "The conversation is too long for the model: too long",
		`{"type":"turn.ended","reason":"failed","error":{"code":"loop.max_steps_exceeded","message":"x","details":{"maxSteps":100}}}`:      "Stopped at the step limit (100)",
		`{"type":"turn.ended","reason":"failed","error":{"code":"provider.api_error","message":"Not Found","details":{"statusCode":404}}}`: "The model was not found: Not Found",
		`{"type":"turn.ended","reason":"failed","error":{"code":"provider.auth_error","message":"Invalid API key"}}`:                       "The provider rejected the sign-in or API key: Invalid API key",
		`{"type":"turn.ended","reason":"blocked"}`: "Kimi blocked this prompt",
	} {
		w := NewWire(harness.Env{})
		got := replay(t, w, []string{`{"type":"turn.prompt","input":[{"type":"text","text":"hi"}],"origin":{"kind":"user"},"time":1}`, line})
		var text string
		failed := false
		for _, it := range got.items {
			if it.Notice != nil && it.Notice.Reason != "" {
				text = it.Notice.Text
			}
			if it.Turn != nil && it.Turn.State == harness.StateFailed {
				failed = true
			}
		}
		if text != want || !failed {
			t.Errorf("%s: notice %q failed=%v, want %q", line, text, failed, want)
		}
	}
}
