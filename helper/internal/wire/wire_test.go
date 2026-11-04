package wire

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParseDecisionAcceptsFrozenForms(t *testing.T) {
	for in, want := range map[string]Decision{
		"approve":                           {Kind: DecisionApprove},
		"deny":                              {Kind: DecisionDeny},
		"option:1":                          {Kind: DecisionOption, N: 1},
		"option:99":                         {Kind: DecisionOption, N: 99},
		"text:hi":                           {Kind: DecisionText, Text: "hi"},
		"text:" + strings.Repeat("é", 2000): {Kind: DecisionText, Text: strings.Repeat("é", 2000)},
		"text:a:b c":                        {Kind: DecisionText, Text: "a:b c"},
	} {
		got, err := ParseDecision(in)
		if err != nil || got != want {
			t.Errorf("ParseDecision(%.20q) = %+v, %v; want %+v", in, got, err, want)
		}
		if got.String() != in {
			t.Errorf("String() = %.20q, want %.20q", got.String(), in)
		}
	}
}

func TestParseDecisionRejects(t *testing.T) {
	for _, in := range []string{
		"option:0", "option:100", "option:01", "option:+5", "option:", "option:x",
		"text:", "text:\x1b[A", "text:a\nb", "text:\u0085", "text:\xff",
		"text:" + strings.Repeat("a", 2001),
		"APPROVE", "", "approve ", "reject",
	} {
		if d, err := ParseDecision(in); err == nil {
			t.Errorf("ParseDecision(%.20q) = %+v, want an error", in, d)
		}
	}
}

func TestParseDecisionErrorsDoNotEchoTheAnswer(t *testing.T) {
	_, err := ParseDecision("text:secret\x1b")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v", err)
	}
}

func TestNewIDsMatchSchemaPatterns(t *testing.T) {
	req := regexp.MustCompile(`^r_[a-z2-7]{10}$`)
	job := regexp.MustCompile(`^j_[a-z2-7]{10}$`)
	seen := map[string]bool{}
	for range 100 {
		r, j := NewRequestID(), NewJobID()
		if !req.MatchString(r) || !job.MatchString(j) {
			t.Fatalf("ids %q %q do not match the schema", r, j)
		}
		if seen[r] {
			t.Fatalf("request id %q repeated", r)
		}
		seen[r] = true
	}
}

func TestEventMarshalsFlat(t *testing.T) {
	sid := "s_abcdefghij"
	ts := Stamp(time.Date(2026, 11, 3, 12, 0, 0, 5e8, time.FixedZone("x", 3600)))
	b, err := json.Marshal(Event{
		Seq: 7, TS: ts, Kind: EventApprovalClosed, SessionID: &sid,
		Payload: ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: ResolutionDesk},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"seq":7,"ts":"2026-11-03T11:00:00Z","kind":"approval.closed","sessionId":"s_abcdefghij","requestId":"r_abcdefghij","resolution":"desk"}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}

	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	var p ApprovalClosedEvent
	if err := json.Unmarshal(back.Payload.(json.RawMessage), &p); err != nil || p.Resolution != ResolutionDesk || back.Seq != 7 || *back.SessionID != sid {
		t.Errorf("round trip: %+v %+v %v", back, p, err)
	}
}

func TestEventWithEmptyPayloadHasOnlyHeader(t *testing.T) {
	b, err := json.Marshal(Event{Seq: 1, Kind: EventSessionRemoved, Payload: SessionRemovedEvent{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"seq":1,"ts":"0001-01-01T00:00:00Z","kind":"session.removed","sessionId":null}`; string(b) != want {
		t.Errorf("got %s", b)
	}
}

func TestEventRejectsPayloadsThatBreakTheShape(t *testing.T) {
	for name, p := range map[string]any{
		"not an object": []int{1},
		"header clash":  map[string]int{"seq": 2},
	} {
		if _, err := json.Marshal(Event{Kind: EventNotify, Payload: p}); err == nil {
			t.Errorf("%s: marshalled without error", name)
		}
	}
}

func TestNullableFieldsEncodeAsNull(t *testing.T) {
	b, _ := json.Marshal(SessionMeta{})
	if string(b) != `{"model":null,"contextPercent":null,"costUsd":null}` {
		t.Errorf("SessionMeta = %s", b)
	}
	b, _ = json.Marshal(AskResult{Reason: AskTimeout})
	if string(b) != `{"decision":null,"reason":"timeout"}` {
		t.Errorf("AskResult = %s", b)
	}
}

func TestEmptySlicesEncodeAsArrays(t *testing.T) {
	b, _ := json.Marshal(SubscribeResult{})
	if string(b) != `{"seq":0,"sessions":[],"approvals":[]}` {
		t.Errorf("SubscribeResult = %s", b)
	}
	b, _ = json.Marshal(Approval{})
	if !strings.Contains(string(b), `"options":[]`) {
		t.Errorf("Approval = %s", b)
	}
}

func TestPushPlaintextOptionsOnlyForNeedsYou(t *testing.T) {
	b, _ := json.Marshal(PushPlaintext{V: 1, Kind: PushNeedsYou, RequestID: "r_abcdefghij", TS: 5})
	if !strings.Contains(string(b), `"options":[]`) || !strings.Contains(string(b), `"sessionId":null`) {
		t.Errorf("needs_you = %s", b)
	}
	b, _ = json.Marshal(PushPlaintext{V: 1, Kind: PushNotify, Title: "t", TS: 5})
	if strings.Contains(string(b), "options") || strings.Contains(string(b), "requestId") {
		t.Errorf("notify = %s", b)
	}
}

func TestApprovalSignedBytesMatchParkedFixture(t *testing.T) {
	got := string(ApprovalSignedBytes("h_mmg42klgyqzwneiskrelxms3j7", "r_abcdefghij", "option:2", 1793707200000))
	if got != "togo-approval/v1\nh_mmg42klgyqzwneiskrelxms3j7\nr_abcdefghij\noption:2\n1793707200000" {
		t.Errorf("got %q", got)
	}
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/answer.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Plaintext AnswerPlaintext }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(f.Plaintext.Sig)
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, 32)).Public().(ed25519.PublicKey)
	msg := ApprovalSignedBytes("h_mmg42klgyqzwneiskrelxms3j7", f.Plaintext.RequestID, f.Plaintext.Decision, f.Plaintext.TS)
	if !ed25519.Verify(pub, msg, sig) {
		t.Error("answer fixture signature does not verify")
	}
}
