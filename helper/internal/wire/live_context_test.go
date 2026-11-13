package wire

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestLiveContextProducerMatchesSignedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/live-context.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Plaintext LiveContextPlaintext `json:"plaintext"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	want := LiveContextPlaintext{Type: PlaintextLiveContext, DeviceID: "dev_test-1", RequestID: "r_abcdefghij", TS: 1793707200000, TokenHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, 32))
	want.Sig = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, LiveContextSignedBytes("h_mmg42klgyqzwneiskrelxms3j7", want.DeviceID, want.RequestID, want.TokenHash, want.TS)))
	if fx.Plaintext != want {
		t.Fatal("producer differs from the signed receipt fixture")
	}
}
