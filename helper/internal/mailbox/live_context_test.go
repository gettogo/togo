package mailbox

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

func contextEnvelope(t *testing.T, f *fixture, request string, ts int64, signer ed25519.PrivateKey) string {
	t.Helper()
	r := wire.LiveContextPlaintext{Type: wire.PlaintextLiveContext, DeviceID: "phone-1", RequestID: request, TS: ts, TokenHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	r.Sig = base64.RawURLEncoding.EncodeToString(ed25519.Sign(signer, wire.LiveContextSignedBytes(f.keys.HostID, r.DeviceID, request, r.TokenHash, ts)))
	raw, _ := json.Marshal(r)
	ct, err := seal.Seal(f.hostPub, raw, seal.LabelAnswer, []byte(f.keys.HostID))
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func TestContextReceiptIsNotAnAnswerOrRegistration(t *testing.T) {
	f := newFixture(t, "")
	v := f.verifier()
	ct := contextEnvelope(t, f, testReq, f.now.UnixMilli(), f.devPriv)
	got, err := v.Open(ct)
	if err != nil || got.Context == nil || got.Answer != nil || got.Register != nil {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	if _, err = v.Open(ct); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: %v", err)
	}
	if _, err = v.Open(f.good(t, "approve")); err != nil {
		t.Fatalf("receipt consumed the answer: %v", err)
	}
}

func TestContextReceiptRejectsUntrustedOrClosedRequest(t *testing.T) {
	for _, kind := range []string{"forged", "old", "future", "closed", "dead", "unpaired"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "")
			ts := f.now.UnixMilli()
			signer := f.devPriv
			switch kind {
			case "forged":
				_, signer, _ = ed25519.GenerateKey(nil)
			case "old":
				ts -= 601000
			case "future":
				ts += 601000
			case "closed":
				delete(f.opts, testReq)
			case "dead":
				f.devices[0].Dead = true
			case "unpaired":
				f.devices = nil
			}
			if _, err := f.verifier().Open(contextEnvelope(t, f, testReq, ts, signer)); err == nil {
				t.Fatal("untrusted receipt accepted")
			}
		})
	}
}

func TestContextReceiptReachesOnlyReadinessHandler(t *testing.T) {
	f := newFixture(t, "")
	r, _ := registerRunner(t, f)
	calls := 0
	r.cfg.ContextReady = func(device, request, tokenHash string, ts int64) error {
		if device != "phone-1" || request != testReq || ts != f.now.UnixMilli() {
			t.Fatal("wrong receipt identity")
		}
		calls++
		return nil
	}
	ct := contextEnvelope(t, f, testReq, f.now.UnixMilli(), f.devPriv)
	r.handle(context.Background(), frame{ID: "context-1", Ciphertext: ct})
	r.handle(context.Background(), frame{ID: "context-1-replay", Ciphertext: ct})
	if calls != 1 {
		t.Fatalf("readiness handler called %d times", calls)
	}
}
