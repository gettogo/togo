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
	b, _ = json.Marshal(RestorableResult{})
	if string(b) != `{"sessions":[]}` {
		t.Errorf("RestorableResult = %s", b)
	}
	b, _ = json.Marshal(HostInfo{Interfaces: []HostInterface{{Name: "en0"}}})
	if string(b) != `{"interfaces":[{"name":"en0","mac":"","ipv4":[]}],"wakeOnLan":null,"routes":[]}` {
		t.Errorf("HostInfo = %s", b)
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

func TestGitWriteNamesMatchSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{MethodGitStage, MethodGitUnstage, MethodGitDiscard, MethodGitCommit} {
		if !bytes.Contains(schema.Defs["Request"], []byte(`{ "const": "`+m+`" }`)) {
			t.Errorf("Request has no dispatch row for %s", m)
		}
	}
	if !bytes.Contains(schema.Defs["HelloResult"], []byte(`\"`+CapGitWrite+`\"`)) {
		t.Errorf("HelloResult does not describe %s", CapGitWrite)
	}
	var data struct {
		Properties struct {
			Reason struct {
				Enum []string `json:"enum"`
			} `json:"reason"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema.Defs["GitConflictData"], &data); err != nil {
		t.Fatal(err)
	}
	want := []string{ConflictStale, ConflictLocked, ConflictFiltered, ConflictUnmerged, ConflictNothingStaged, ConflictIdentity, ConflictUnbornAmend, ConflictPartial, ConflictRaced, ConflictCaseRename}
	if strings.Join(data.Properties.Reason.Enum, ",") != strings.Join(want, ",") {
		t.Errorf("GitConflictData reasons = %v, want %v", data.Properties.Reason.Enum, want)
	}
}

func TestFingerprintFormsMatchSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			GitFingerprint struct {
				Pattern string `json:"pattern"`
			} `json:"GitFingerprint"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(schema.Defs.GitFingerprint.Pattern)
	for _, fp := range []string{
		BlobFingerprint("13ab7f7412573d479aa8b41ce1e29a9f9f2a62d5"),
		StatFingerprint(9_000_000, 1_793_707_200_123_456_789),
	} {
		if !pattern.MatchString(fp) {
			t.Errorf("%q does not match %s", fp, pattern)
		}
	}
	if got := StatFingerprint(9_000_000, 5); got != "stat:9000000:5" {
		t.Errorf("StatFingerprint = %q", got)
	}
	if !HashesForFingerprint(MaxBlobFingerprintBytes) || HashesForFingerprint(MaxBlobFingerprintBytes+1) {
		t.Errorf("blob fingerprints stop at %d bytes", MaxBlobFingerprintBytes)
	}
}

func TestGitSeenEncodesStatusAndOptionalFingerprint(t *testing.T) {
	b, _ := json.Marshal(map[string]GitSeen{"a": {Status: ".M", Fingerprint: "blob:ab"}, "b/": {Status: "??"}})
	if want := `{"a":{"status":".M","fingerprint":"blob:ab"},"b/":{"status":"??"}}`; string(b) != want {
		t.Errorf("got %s", b)
	}
}

func TestStatusFileCapMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			GitStatusResult struct {
				Properties struct {
					Files struct {
						MaxItems int `json:"maxItems"`
					} `json:"files"`
				} `json:"properties"`
			} `json:"GitStatusResult"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Defs.GitStatusResult.Properties.Files.MaxItems; got != MaxStatusFiles {
		t.Errorf("schema caps git.status files at %d, MaxStatusFiles = %d", got, MaxStatusFiles)
	}
}

func TestWritePathsNeverNameAFolderEntry(t *testing.T) {
	for p, want := range map[string]bool{"a.txt": true, "dir/a.txt": true, "newpkg/": false, "/": false, "": false} {
		if got := IsFileEntry(p); got != want {
			t.Errorf("IsFileEntry(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestPairParamsPlatformDefaultsIOS(t *testing.T) {
	old := `{"deviceId":"dev_test-1","name":"Test iPhone","apnsToken":"ab","env":"sandbox","relayUrl":"https://relay.example.test"}`
	var p PairParams
	if err := json.Unmarshal([]byte(old), &p); err != nil {
		t.Fatal(err)
	}
	if p.EffectivePlatform() != PlatformIOS || p.LiveActivityCommand || p.LiveActivityStartToken != "" {
		t.Errorf("old params = %+v", p)
	}

	var w PairParams
	if err := json.Unmarshal([]byte(`{"deviceId":"w","platform":"watchos"}`), &w); err != nil || w.EffectivePlatform() != PlatformWatch {
		t.Errorf("watch params = %+v, %v", w, err)
	}

	stored := `{"deviceId":"dev_test-1","env":"sandbox","pairedAt":"2026-11-03T12:00:00Z"}`
	var d Device
	if err := json.Unmarshal([]byte(stored), &d); err != nil {
		t.Fatal(err)
	}
	if d.EffectivePlatform() != PlatformIOS || d.DeviceID != "dev_test-1" || !d.PairedAt.Equal(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("stored device = %+v", d)
	}
}

func TestPairParamsOmitUnsetAdditions(t *testing.T) {
	b, _ := json.Marshal(PairParams{DeviceID: "d"})
	for _, k := range []string{"platform", "liveActivityStartToken", "liveActivityCommand"} {
		if strings.Contains(string(b), k) {
			t.Errorf("zero params carry %s: %s", k, b)
		}
	}
}

func TestRegisterSignedBytesExact(t *testing.T) {
	const host, dev, ts = "h_mmg42klgyqzwneiskrelxms3j7", "dev_test-1", int64(1793707200000)
	apns, la, start := strings.Repeat("ab", 32), strings.Repeat("cd", 32), strings.Repeat("ef", 32)
	got := string(RegisterSignedBytes(host, dev, apns, la, start, true, ts))
	want := "togo-register/v2\nh_mmg42klgyqzwneiskrelxms3j7\ndev_test-1\n" + apns + "\n" + la + "\n" + start + "\ntrue\n1793707200000"
	if got != want {
		t.Errorf("all tokens:\ngot  %q\nwant %q", got, want)
	}
	got = string(RegisterSignedBytes(host, dev, "", la, "", false, ts))
	want = "togo-register/v2\nh_mmg42klgyqzwneiskrelxms3j7\ndev_test-1\n\n" + la + "\n\nfalse\n1793707200000"
	if got != want {
		t.Errorf("one token:\ngot  %q\nwant %q", got, want)
	}
}

func TestRegisterFixtureSignatureVerifies(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/register.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Plaintext RegisterPlaintext }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	p := f.Plaintext
	if p.Type != PlaintextRegister {
		t.Errorf("type = %q", p.Type)
	}
	sig, err := base64.RawURLEncoding.DecodeString(p.Sig)
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, 32)).Public().(ed25519.PublicKey)
	if p.LiveActivityCommand == nil {
		t.Fatal("register fixture has no liveActivityCommand")
	}
	msg := RegisterSignedBytes("h_mmg42klgyqzwneiskrelxms3j7", p.DeviceID, p.APNsToken, p.LiveActivityToken, p.LiveActivityStartToken, *p.LiveActivityCommand, p.TS)
	if !ed25519.Verify(pub, msg, sig) {
		t.Error("register fixture signature does not verify")
	}
}

func TestPushPlaintextSubjectOmittedWhenNil(t *testing.T) {
	b, _ := json.Marshal(PushPlaintext{V: 1, Kind: PushNeedsYou, RequestID: "r_abcdefghij", TS: 5})
	if strings.Contains(string(b), "subject") || strings.Contains(string(b), "host") {
		t.Errorf("without subject = %s", b)
	}
	host := "studio"
	b, _ = json.Marshal(PushPlaintext{V: 1, Kind: PushNeedsYou, RequestID: "r_abcdefghij", TS: 5,
		Subject: &Subject{Kind: SubjectCommand, Text: "npm test"}, Host: &host})
	if !strings.Contains(string(b), `"subject":{"kind":"command","text":"npm test"}`) || !strings.Contains(string(b), `"host":"studio"`) {
		t.Errorf("with subject = %s", b)
	}
}

func TestApprovalSubjectEncodesNull(t *testing.T) {
	b, _ := json.Marshal(Approval{})
	if !strings.Contains(string(b), `"subject":null`) {
		t.Errorf("Approval = %s", b)
	}
}

func TestAnswerPlaintextAcceptsTypeAnswer(t *testing.T) {
	for _, in := range []string{
		`{"requestId":"r_abcdefghij","decision":"approve","ts":1,"deviceId":"d","sig":"x"}`,
		`{"type":"answer","requestId":"r_abcdefghij","decision":"approve","ts":1,"deviceId":"d","sig":"x"}`,
	} {
		dec := json.NewDecoder(strings.NewReader(in))
		dec.DisallowUnknownFields()
		var a AnswerPlaintext
		if err := dec.Decode(&a); err != nil || a.RequestID != "r_abcdefghij" {
			t.Errorf("%s: %+v, %v", in, a, err)
		}
	}
	for _, typ := range []string{"", PlaintextAnswer} {
		b, _ := json.Marshal(AnswerPlaintext{Type: typ, RequestID: "r_abcdefghij"})
		if strings.Contains(string(b), "type") {
			t.Errorf("encoded answer carries type: %s", b)
		}
	}
}

func TestValidWritePath(t *testing.T) {
	for p, want := range map[string]bool{
		"a.txt":                                        true,
		"dir/naïve file.txt":                           true,
		strings.Repeat("a", MaxWritePathBytes):         true,
		strings.Repeat("a", MaxWritePathBytes+1):       false,
		strings.Repeat("é", MaxWritePathBytes/2) + "a": false, // 2,049 characters but 4,097 bytes
		"a\x00b":  false,
		"newpkg/": false,
		"":        false,
	} {
		if got := ValidWritePath(p); got != want {
			t.Errorf("ValidWritePath(%.20q, %d bytes) = %v, want %v", p, len(p), got, want)
		}
	}
}

func TestValidCommitHash(t *testing.T) {
	for h, want := range map[string]bool{
		strings.Repeat("a", 40): true, strings.Repeat("0", 64): true,
		strings.Repeat("a", 41): false, strings.Repeat("a", 63): false, strings.Repeat("A", 40): false,
		strings.Repeat("g", 40): false, "": false,
	} {
		if got := ValidCommitHash(h); got != want {
			t.Errorf("ValidCommitHash(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestSubjectValidate(t *testing.T) {
	for _, s := range []Subject{
		{Kind: SubjectCommand, Text: "npm test"},
		{Kind: SubjectFile, Text: "/home/u/naïve.go"},
		{Kind: SubjectURL, Text: "https://example.test/a"},
		{Kind: SubjectText, Text: "line one\nline two"},
		{Kind: SubjectText, Text: strings.Repeat("é", MaxSubjectText)},
	} {
		if err := s.Validate(); err != nil {
			t.Errorf("%+.20v: %v", s, err)
		}
	}
	for _, s := range []Subject{
		{Kind: "path", Text: "x"},
		{Kind: SubjectCommand, Text: ""},
		{Kind: SubjectCommand, Text: "a\tb"},
		{Kind: SubjectCommand, Text: "a\x1b[2Jb"},
		{Kind: SubjectCommand, Text: "a\rb"},
		{Kind: SubjectCommand, Text: "\u0085"},
		{Kind: SubjectCommand, Text: "rm -rf ~/x\u202Egnp.txt"},
		{Kind: SubjectCommand, Text: "npm\u200Btest"},
		{Kind: SubjectCommand, Text: "\u2066ls\u2069"},
		{Kind: SubjectCommand, Text: "\ufeffls"},
		{Kind: SubjectCommand, Text: "\xff"},
		{Kind: SubjectText, Text: strings.Repeat("a", MaxSubjectText+1)},
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%+.20q validated", s.Text)
		}
	}
}

func TestWritePathLimitMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			GitWritePath struct {
				MaxLength int `json:"maxLength"`
			} `json:"GitWritePath"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Defs.GitWritePath.MaxLength != MaxWritePathBytes {
		t.Errorf("schema maxLength %d, MaxWritePathBytes %d", schema.Defs.GitWritePath.MaxLength, MaxWritePathBytes)
	}
}

func TestSubjectDecodeValidates(t *testing.T) {
	var s Subject
	if err := json.Unmarshal([]byte(`{"kind":"command","text":"npm test\nnpm run lint"}`), &s); err != nil || s.Text != "npm test\nnpm run lint" {
		t.Errorf("good subject: %+v, %v", s, err)
	}
	// 1024 scalars of a 2-byte character pass; the limit counts scalars, not bytes.
	if err := json.Unmarshal([]byte(`{"kind":"text","text":"`+strings.Repeat("é", MaxSubjectText)+`"}`), &s); err != nil {
		t.Errorf("1024 scalars: %v", err)
	}
	for name, raw := range map[string]string{
		"empty":     `{"kind":"text","text":""}`,
		"too long":  `{"kind":"text","text":"` + strings.Repeat("a", MaxSubjectText+1) + `"}`,
		"esc":       `{"kind":"command","text":"a\u001b[2Jb"}`,
		"cr":        `{"kind":"command","text":"a\rb"}`,
		"tab":       `{"kind":"command","text":"a\tb"}`,
		"del":       `{"kind":"command","text":"a\u007fb"}`,
		"c1":        `{"kind":"command","text":"a\u0085b"}`,
		"kind":      `{"kind":"diff","text":"x"}`,
		"in a push": `{"v":1,"kind":"needs_you","subject":{"kind":"command","text":"\u0007"}}`,
	} {
		var v any = &Subject{}
		if name == "in a push" {
			v = &PushPlaintext{}
		}
		if err := json.Unmarshal([]byte(raw), v); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
}
