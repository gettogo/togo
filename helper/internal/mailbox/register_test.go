package mailbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

var (
	regAPNs  = strings.Repeat("ab", 32)
	regLive  = strings.Repeat("cd", 32)
	regStart = strings.Repeat("ef", 32)
)

type registered struct {
	device, apns, la, laStart string
	command                   bool
}

// register seals a signed registration the way the phone does.
func (f *fixture) register(t *testing.T, r wire.RegisterPlaintext, signer ed25519.PrivateKey) string {
	t.Helper()
	r.Type = wire.PlaintextRegister
	if r.DeviceID == "" {
		r.DeviceID = "phone-1"
	}
	if r.LiveActivityCommand == nil {
		r.LiveActivityCommand = new(bool)
	}
	sig := ed25519.Sign(signer, wire.RegisterSignedBytes(f.keys.HostID, r.DeviceID, r.APNsToken,
		r.LiveActivityToken, r.LiveActivityStartToken, *r.LiveActivityCommand, r.TS))
	r.Sig = base64.RawURLEncoding.EncodeToString(sig)
	pt, _ := json.Marshal(r)
	ct, err := seal.Seal(f.hostPub, pt, seal.LabelAnswer, []byte(f.keys.HostID))
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

// registerRunner is a runner whose Register records into the returned slice
// and whose Answer fails the test.
func registerRunner(t *testing.T, f *fixture) (*runner, *[]registered) {
	var got []registered
	r := &runner{cfg: Config{
		Logf: func(string, ...any) {},
		Answer: func(context.Context, string, string, string) error {
			t.Error("registration reached the answer path")
			return nil
		},
		Register: func(device, apns, la, laStart string, command bool, _ int64) error {
			got = append(got, registered{device, apns, la, laStart, command})
			return nil
		},
	}, v: f.verifier()}
	return r, &got
}

func TestRegisterUpdatesTokens(t *testing.T) {
	f := newFixture(t, "")
	r, got := registerRunner(t, f)
	ct := f.register(t, wire.RegisterPlaintext{LiveActivityToken: regLive, TS: f.now.UnixMilli()}, f.devPriv)
	if !r.handle(context.Background(), frame{ID: "m1", Ciphertext: ct}) {
		t.Fatal("registration not acked")
	}
	all := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, LiveActivityToken: regLive,
		LiveActivityStartToken: regStart, TS: f.now.UnixMilli() + 1}, f.devPriv)
	r.handle(context.Background(), frame{ID: "m2", Ciphertext: all})
	want := []registered{{"phone-1", "", regLive, "", false}, {"phone-1", regAPNs, regLive, regStart, false}}
	if len(*got) != 2 || (*got)[0] != want[0] || (*got)[1] != want[1] {
		t.Fatalf("registered %+v", *got)
	}
}

func TestRegisterForgedSignatureDropped(t *testing.T) {
	f := newFixture(t, "")
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	r, got := registerRunner(t, f)
	ct := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: f.now.UnixMilli()}, other)
	if !r.handle(context.Background(), frame{ID: "m1", Ciphertext: ct}) {
		t.Fatal("forged registration not acked")
	}
	if _, err := f.verifier().Open(ct); !errors.Is(err, ErrSignature) {
		t.Fatalf("forged: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("forged registration applied: %+v", *got)
	}
}

func TestRegisterReplayDropped(t *testing.T) {
	f := newFixture(t, "")
	r, got := registerRunner(t, f)
	ct := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: f.now.UnixMilli()}, f.devPriv)
	r.handle(context.Background(), frame{ID: "m1", Ciphertext: ct})
	r.handle(context.Background(), frame{ID: "m2", Ciphertext: ct})
	if len(*got) != 1 {
		t.Fatalf("replayed registration applied %d times", len(*got))
	}
	if _, err := r.v.Open(ct); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: %v", err)
	}
	// The registration's replay key does not collide with answers.
	if _, _, err := r.v.Verify(f.good(t, "approve")); err != nil {
		t.Fatalf("answer after registration: %v", err)
	}
}

func TestRegisterRejects(t *testing.T) {
	f := newFixture(t, "")
	now := f.now.UnixMilli()
	cases := []struct {
		name string
		reg  wire.RegisterPlaintext
		want error
	}{
		{"stale", wire.RegisterPlaintext{APNsToken: regAPNs, TS: now - 601_000}, ErrStale},
		{"future", wire.RegisterPlaintext{APNsToken: regAPNs, TS: now + 601_000}, ErrStale},
		{"uppercase token", wire.RegisterPlaintext{APNsToken: strings.ToUpper(regAPNs), TS: now}, ErrMalformed},
		{"short token", wire.RegisterPlaintext{LiveActivityToken: "abcd", TS: now}, ErrMalformed},
		{"unknown device", wire.RegisterPlaintext{DeviceID: "phone-2", APNsToken: regAPNs, TS: now}, ErrUnknownDevice},
	}
	for _, c := range cases {
		if _, err := f.verifier().Open(f.register(t, c.reg, f.devPriv)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	f.devices[0].Dead = true
	if _, err := f.verifier().Open(f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: now}, f.devPriv)); !errors.Is(err, ErrUnknownDevice) {
		t.Errorf("dead device: %v", err)
	}
}

func TestUnknownTypeAndRegisterThroughVerifyRefused(t *testing.T) {
	f := newFixture(t, "")
	pt := []byte(`{"type":"other","deviceId":"phone-1","ts":1,"sig":"x"}`)
	ct, _ := seal.Seal(f.hostPub, pt, seal.LabelAnswer, []byte(f.keys.HostID))
	if _, err := f.verifier().Open(ct); !errors.Is(err, ErrMalformed) {
		t.Fatalf("unknown type: %v", err)
	}
	reg := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: f.now.UnixMilli()}, f.devPriv)
	if _, _, err := f.verifier().Verify(reg); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Verify accepted a registration: %v", err)
	}
	// An answer may carry type "answer".
	sig := ed25519.Sign(f.devPriv, wire.ApprovalSignedBytes(f.keys.HostID, testReq, "approve", f.now.UnixMilli()))
	raw, _ := json.Marshal(map[string]any{"type": "answer", "requestId": testReq, "decision": "approve",
		"ts": f.now.UnixMilli(), "deviceId": "phone-1", "sig": base64.RawURLEncoding.EncodeToString(sig)})
	ct, _ = seal.Seal(f.hostPub, raw, seal.LabelAnswer, []byte(f.keys.HostID))
	if _, _, err := f.verifier().Verify(ct); err != nil {
		t.Fatalf("typed answer: %v", err)
	}
}

func TestRegisterOlderThanLastAppliedDropped(t *testing.T) {
	f := newFixture(t, "")
	f.devices[0].RegisterTS = f.now.UnixMilli()
	r, got := registerRunner(t, f)
	for i, ts := range []int64{f.now.UnixMilli() - 1000, f.now.UnixMilli()} {
		ct := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: ts}, f.devPriv)
		if _, err := f.verifier().Open(ct); !errors.Is(err, ErrReplay) {
			t.Fatalf("case %d: %v, want replay", i, err)
		}
		r.handle(context.Background(), frame{ID: "m", Ciphertext: ct})
	}
	if len(*got) != 0 {
		t.Fatalf("older registration applied: %+v", *got)
	}
	ct := f.register(t, wire.RegisterPlaintext{APNsToken: regAPNs, TS: f.now.UnixMilli() + 1}, f.devPriv)
	if _, err := f.verifier().Open(ct); err != nil {
		t.Fatalf("newer registration: %v", err)
	}
}

func TestRegisterCarriesCommandChoice(t *testing.T) {
	f := newFixture(t, "")
	r, got := registerRunner(t, f)
	on := true
	ct := f.register(t, wire.RegisterPlaintext{LiveActivityCommand: &on, TS: f.now.UnixMilli()}, f.devPriv)
	r.handle(context.Background(), frame{ID: "m1", Ciphertext: ct})
	if len(*got) != 1 || !(*got)[0].command {
		t.Fatalf("registered %+v", *got)
	}
}

func TestRegisterWithoutCommandChoiceRefused(t *testing.T) {
	f := newFixture(t, "")
	sig := ed25519.Sign(f.devPriv, wire.RegisterSignedBytes(f.keys.HostID, "phone-1", regAPNs, "", "", false, f.now.UnixMilli()))
	raw, _ := json.Marshal(map[string]any{"type": "register", "deviceId": "phone-1", "apnsToken": regAPNs,
		"ts": f.now.UnixMilli(), "sig": base64.RawURLEncoding.EncodeToString(sig)})
	ct, _ := seal.Seal(f.hostPub, raw, seal.LabelAnswer, []byte(f.keys.HostID))
	if _, err := f.verifier().Open(ct); !errors.Is(err, ErrMalformed) {
		t.Fatalf("missing liveActivityCommand: %v", err)
	}
}

func TestRegisterCommandChoiceIsSigned(t *testing.T) {
	f := newFixture(t, "")
	off, on := false, true
	sig := ed25519.Sign(f.devPriv, wire.RegisterSignedBytes(f.keys.HostID, "phone-1", "", "", "", off, f.now.UnixMilli()))
	r := wire.RegisterPlaintext{Type: wire.PlaintextRegister, DeviceID: "phone-1", LiveActivityCommand: &on,
		TS: f.now.UnixMilli(), Sig: base64.RawURLEncoding.EncodeToString(sig)}
	raw, _ := json.Marshal(r)
	ct, _ := seal.Seal(f.hostPub, raw, seal.LabelAnswer, []byte(f.keys.HostID))
	if _, err := f.verifier().Open(ct); !errors.Is(err, ErrSignature) {
		t.Fatalf("flipped choice: %v", err)
	}
}
