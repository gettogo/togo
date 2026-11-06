package wire

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSignalExitRoundTrip(t *testing.T) {
	zero := 0
	b, err := json.Marshal(Signal{SessionID: "s_abcdefghij", Kind: SignalExit, Code: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"sessionId":"s_abcdefghij","kind":"exit","code":0}`; string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	var back Signal
	if err := json.Unmarshal(b, &back); err != nil || back.Code == nil || *back.Code != 0 || back.Kind != SignalExit {
		t.Errorf("round trip: %+v, %v", back, err)
	}
	b, _ = json.Marshal(Signal{SessionID: "s_abcdefghij", Kind: SignalBell})
	if strings.Contains(string(b), "code") {
		t.Errorf("bell carries code: %s", b)
	}
}

func TestPushPlaintextOptionalFieldsOmitted(t *testing.T) {
	b, _ := json.Marshal(PushPlaintext{V: 1, Kind: PushDone, Title: "t", TS: 5})
	for _, k := range []string{`"exit"`, `"active"`, `"reminder"`} {
		if strings.Contains(string(b), k) {
			t.Errorf("done push carries %s: %s", k, b)
		}
	}
	active, code := 2, 1
	b, _ = json.Marshal(PushPlaintext{V: 1, Kind: PushOffline, TS: 5, Active: &active})
	if !strings.Contains(string(b), `"active":2`) {
		t.Errorf("offline = %s", b)
	}
	b, _ = json.Marshal(PushPlaintext{V: 1, Kind: PushFailed, TS: 5, Exit: &SessionExit{Code: &code, Reason: ExitReasonExit}})
	if !strings.Contains(string(b), `"exit":{"code":1,"reason":"exit","detail":null}`) {
		t.Errorf("failed = %s", b)
	}
	b, _ = json.Marshal(PushPlaintext{V: 1, Kind: PushNeedsYou, RequestID: "r_abcdefghij", TS: 5, Reminder: true})
	if !strings.Contains(string(b), `"reminder":true`) {
		t.Errorf("reminder = %s", b)
	}
}

func TestDeviceStoresPairedSSHKey(t *testing.T) {
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER"
	d := Device{PairParams: PairParams{DeviceID: "dev_test-1", SSHKey: key}, PairedAt: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	b, err := json.Marshal(d)
	if err != nil || !strings.Contains(string(b), `"sshKey":"`+key+`"`) {
		t.Fatalf("encoded = %s, %v", b, err)
	}
	var back Device
	if err := json.Unmarshal(b, &back); err != nil || back.SSHKey != key || back.PairParams.SSHKey != key {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}

func TestSessionExitValidate(t *testing.T) {
	str := func(s string) *string { return &s }
	zero, big := 0, 256
	for _, e := range []SessionExit{
		{Code: &zero, Reason: ExitReasonExit},
		{Reason: ExitReasonUsageLimit, Detail: str("Usage limit reached")},
		{Reason: ExitReasonError, Detail: str(strings.Repeat("é", MaxExitDetail))},
	} {
		if err := e.Validate(); err != nil {
			t.Errorf("%+v: %v", e, err)
		}
	}
	for name, e := range map[string]SessionExit{
		"reason":    {Reason: "crashed"},
		"code":      {Code: &big, Reason: ExitReasonExit},
		"empty":     {Reason: ExitReasonError, Detail: str("")},
		"too long":  {Reason: ExitReasonError, Detail: str(strings.Repeat("a", MaxExitDetail+1))},
		"newline":   {Reason: ExitReasonError, Detail: str("a\nb")},
		"escape":    {Reason: ExitReasonError, Detail: str("a\x1b[31mb")},
		"c1":        {Reason: ExitReasonError, Detail: str("a\u0085b")},
		"bidi":      {Reason: ExitReasonError, Detail: str("a\u202eb")},
		"zero":      {Reason: ExitReasonError, Detail: str("a\u200bb")},
		"box":       {Reason: ExitReasonError, Detail: str("\u2502 limit \u2502")},
		"not utf-8": {Reason: ExitReasonError, Detail: str("\xff")},
	} {
		if err := e.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
	var p PushPlaintext
	if err := json.Unmarshal([]byte(`{"v":1,"kind":"failed","exit":{"code":null,"reason":"error","detail":"a\u001b[2Jb"}}`), &p); err == nil {
		t.Error("push with a dirty exit detail decoded")
	}
}

func TestDeviceSSHKeyOmittedWhenEmpty(t *testing.T) {
	b, _ := json.Marshal(Device{})
	if strings.Contains(string(b), "sshKey") {
		t.Errorf("device = %s", b)
	}
}

// fixtureSession mirrors the protocol Session, which the session package
// owns, so that session results in the new fixtures decode strictly here.
type fixtureSession struct {
	ID              string       `json:"id"`
	Bib             *string      `json:"bib"`
	Name            string       `json:"name"`
	Agent           string       `json:"agent"`
	Cwd             string       `json:"cwd"`
	CreatedAt       time.Time    `json:"createdAt"`
	Status          string       `json:"status"`
	Activity        *string      `json:"activity"`
	AttachedClients int          `json:"attachedClients"`
	Managed         bool         `json:"managed"`
	Muted           bool         `json:"muted"`
	Meta            SessionMeta  `json:"meta"`
	Command         []string     `json:"command"`
	Exit            *SessionExit `json:"exit"`
}

func strictDecode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func TestNewFixturesDecode(t *testing.T) {
	type pair struct{ params, result func() any }
	sess := func() any { return &fixtureSession{} }
	empty := func() any { return &struct{}{} }
	ref := func() any { return &SessionRefParams{} }
	for name, p := range map[string]pair{
		"session.create-args":      {func() any { return &SessionCreateParams{} }, sess},
		"session.create-custom":    {func() any { return &SessionCreateParams{} }, sess},
		"session.create-bad-args":  {func() any { return &SessionCreateParams{} }, nil},
		"agents.check":             {func() any { return &AgentsCheckParams{} }, func() any { return &AgentsCheckResult{} }},
		"session.relaunch":         {ref, sess},
		"session.relaunch-running": {ref, nil},
		"session.list-ended":       {empty, func() any { return &struct{ Sessions []fixtureSession }{} }},
		"session.restorable":       {empty, func() any { return &RestorableResult{} }},
		"session.restore":          {func() any { return &SessionRestoreParams{} }, sess},
		"session.dismiss":          {ref, empty},
		"session.sendText":         {func() any { return &SessionSendTextParams{} }, empty},
		"session.sendText-shell":   {func() any { return &SessionSendTextParams{} }, nil},
		"notify.get":               {empty, func() any { return &NotifySettings{} }},
		"notify.set":               {func() any { return &NotifySetParams{} }, func() any { return &NotifySettings{} }},
		"notify.set-bad":           {func() any { return &NotifySetParams{} }, nil},
		"authkeys.remove":          {func() any { return &AuthkeysRemoveParams{} }, func() any { return &AuthkeysRemoveResult{} }},
		"authkeys.remove-symlink":  {func() any { return &AuthkeysRemoveParams{} }, nil},
		"host.info":                {empty, func() any { return &HostInfo{} }},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../protocol/v1/fixtures/pending/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Request struct {
					Params json.RawMessage `json:"params"`
				} `json:"request"`
				Response struct {
					Result json.RawMessage `json:"result"`
					Error  *struct {
						Data struct {
							Reason string `json:"reason"`
						} `json:"data"`
					} `json:"error"`
				} `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			if err := strictDecode(fx.Request.Params, p.params()); err != nil {
				t.Errorf("params: %v", err)
			}
			if p.result == nil {
				if fx.Response.Error == nil || fx.Response.Error.Data.Reason == "" {
					t.Errorf("want an error with a reason, got %s", raw)
				}
				return
			}
			if err := strictDecode(fx.Response.Result, p.result()); err != nil {
				t.Errorf("result: %v", err)
			}
		})
	}
	for _, name := range []string{"push.ended", "push.failed", "push.offline", "push.reminder"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var fx struct{ Plaintext json.RawMessage }
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			var p PushPlaintext
			if err := strictDecode(fx.Plaintext, &p); err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(p)
			var a, b any
			_ = json.Unmarshal(fx.Plaintext, &a)
			_ = json.Unmarshal(again, &b)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("re-encoded differs:\n got  %s\n want %s", again, fx.Plaintext)
			}
		})
	}
}

func TestPairParamsSSHKeyDecodes(t *testing.T) {
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBERERERERERERERERERERERERERERERERERERERERER"
	raw := `{"deviceId":"dev_test-1","name":"Test iPhone","x25519":"x","approvalKey":"a","apnsToken":"ab","env":"sandbox","relayUrl":"https://relay.example.test","sshKey":"` + key + `"}`
	var p PairParams
	if err := strictDecode(json.RawMessage(raw), &p); err != nil || p.SSHKey != key {
		t.Errorf("params = %+v, %v", p, err)
	}
	b, _ := json.Marshal(PairParams{DeviceID: "d"})
	if strings.Contains(string(b), "sshKey") {
		t.Errorf("zero params carry sshKey: %s", b)
	}
}
