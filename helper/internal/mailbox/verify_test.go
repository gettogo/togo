package mailbox

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// fixture is a host plus one paired device.
type fixture struct {
	keys    Keys
	hostPub [32]byte
	devPriv ed25519.PrivateKey
	devices []Device
	opts    map[string][]wire.Option
	now     time.Time
}

const testReq = "r_abcdefghij"

func newFixture(t *testing.T, relayURL string) *fixture {
	t.Helper()
	priv, pub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	dpub, dpriv, _ := ed25519.GenerateKey(rand.Reader)
	return &fixture{
		keys:    Keys{HostID: seal.HostID(secret), Private: priv, MailboxSecret: secret},
		hostPub: pub,
		devPriv: dpriv,
		devices: []Device{{DeviceID: "phone-1", ApprovalKey: dpub, RelayURL: relayURL}},
		opts: map[string][]wire.Option{testReq: {
			{N: 1, Label: "Yes", Role: wire.RoleApprove},
			{N: 2, Label: "Yes, always", Role: wire.RoleApproveAlways},
			{N: 3, Label: "No", Role: wire.RoleDeny},
		}},
		now: time.UnixMilli(1_800_000_000_000),
	}
}

func (f *fixture) verifier() *Verifier {
	return NewVerifier(f.keys, func() []Device { return f.devices },
		func(id string) ([]wire.Option, bool) { o, ok := f.opts[id]; return o, ok },
		func() time.Time { return f.now })
}

// answer seals a signed answer the way the phone does.
func (f *fixture) answer(t *testing.T, req, decision string, ts int64, signer ed25519.PrivateKey) string {
	t.Helper()
	sig := ed25519.Sign(signer, wire.ApprovalSignedBytes(f.keys.HostID, req, decision, ts))
	pt, _ := json.Marshal(wire.AnswerPlaintext{RequestID: req, Decision: decision, TS: ts, DeviceID: "phone-1",
		Sig: base64.RawURLEncoding.EncodeToString(sig)})
	ct, err := seal.Seal(f.hostPub, pt, seal.LabelAnswer, []byte(f.keys.HostID))
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func (f *fixture) good(t *testing.T, decision string) string {
	return f.answer(t, testReq, decision, f.now.UnixMilli(), f.devPriv)
}

func TestVerifyAcceptsValidAnswers(t *testing.T) {
	for _, d := range []string{"approve", "deny", "option:1", "option:3"} {
		f := newFixture(t, "")
		req, dec, err := f.verifier().Verify(f.good(t, d))
		if err != nil || req != testReq || dec != d {
			t.Fatalf("%s: got %q %q %v", d, req, dec, err)
		}
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFixture(t, "")
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	now := f.now.UnixMilli()
	cases := []struct {
		name string
		ct   func() string
		want error
	}{
		{"forged signature", func() string { return f.answer(t, testReq, "approve", now, other) }, ErrSignature},
		{"stale", func() string { return f.answer(t, testReq, "approve", now-601_000, f.devPriv) }, ErrStale},
		{"future", func() string { return f.answer(t, testReq, "approve", now+601_000, f.devPriv) }, ErrStale},
		{"text", func() string { return f.good(t, "text:yes") }, ErrDecision},
		{"approve always", func() string { return f.good(t, "option:2") }, ErrDecision},
		{"option not offered", func() string { return f.good(t, "option:9") }, ErrDecision},
		{"option on unknown request", func() string {
			return f.answer(t, "r_zzzzzzzzzz", "option:1", now, f.devPriv)
		}, ErrDecision},
		{"bad decision", func() string { return f.good(t, "maybe") }, ErrMalformed},
		{"bad request id", func() string { return f.answer(t, "x", "approve", now, f.devPriv) }, ErrMalformed},
		{"garbage", func() string { return "AQID" }, ErrOpen},
		{"wrong label", func() string {
			ct, _ := seal.Seal(f.hostPub, []byte("{}"), seal.LabelPush, []byte(f.keys.HostID))
			return ct
		}, ErrOpen},
	}
	for _, c := range cases {
		if _, _, err := f.verifier().Verify(c.ct()); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestVerifyRevokedOrUnknownDevice(t *testing.T) {
	f := newFixture(t, "")
	ct := f.good(t, "approve")
	f.devices[0].Dead = true
	if _, _, err := f.verifier().Verify(ct); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("dead device: %v", err)
	}
	f.devices = nil
	if _, _, err := f.verifier().Verify(ct); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("no device: %v", err)
	}
}

func TestVerifyReplayAndPrune(t *testing.T) {
	f := newFixture(t, "")
	v := f.verifier()
	ct := f.good(t, "approve")
	if _, _, err := v.Verify(ct); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Verify(ct); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: %v", err)
	}
	// A second, freshly signed answer for the same request is refused too.
	f.now = f.now.Add(time.Second)
	if _, _, err := v.Verify(f.good(t, "deny")); !errors.Is(err, ErrReplay) {
		t.Fatalf("second answer: %v", err)
	}
	// Once the first answer's ts has left the window the entry is pruned,
	// and the old ciphertext now fails as stale instead.
	f.now = f.now.Add(FreshnessWindow)
	if _, _, err := v.Verify(ct); !errors.Is(err, ErrStale) {
		t.Fatalf("after window: %v", err)
	}
	// The next accepted answer prunes the expired entry, leaving only its own.
	if _, _, err := v.Verify(f.answer(t, "r_bbbbbbbbbb", "approve", f.now.UnixMilli(), f.devPriv)); err != nil {
		t.Fatal(err)
	}
	v.mu.Lock()
	_, first := v.seen[testReq]
	n := len(v.seen)
	v.mu.Unlock()
	if n != 1 || first {
		t.Fatalf("seen has %d entries (testReq kept: %v)", n, first)
	}
}

func TestBackoffBounds(t *testing.T) {
	minD, maxD := time.Second, 30*time.Second
	for a, want := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		want *= time.Second
		lo := backoff(a, minD, maxD, func() float64 { return 0 })
		hi := backoff(a, minD, maxD, func() float64 { return 0.999999 })
		if lo != want/2 || hi > want || hi < want*9/10 {
			t.Errorf("attempt %d: [%v, %v], want [%v, %v]", a, lo, hi, want/2, want)
		}
	}
}
