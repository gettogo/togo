package seal

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	priv, pub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	if derived := mustPub(t, priv); derived != pub {
		t.Fatal("GenerateX25519 public key does not match PublicKey(priv)")
	}
	msg := []byte(`{"v":1,"kind":"notify"}`)
	aad := []byte(fixtureHostID + "|dev_test-1")
	env, err := Seal(pub, msg, LabelPush, aad)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(env)
	if err != nil || raw[0] != Version || len(raw) != headerSize+len(msg)+tagSize {
		t.Fatalf("unexpected envelope layout: len %d err %v", len(raw), err)
	}
	got, err := Open(priv, env, LabelPush, aad)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := Open(priv, env, LabelAnswer, aad); err != ErrOpen {
		t.Fatalf("push envelope opened under the answer label: %v", err)
	}
}

func TestSealUsesFreshEphemeralAndNonce(t *testing.T) {
	_, pub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 64 {
		env, err := Seal(pub, []byte("same"), LabelPush, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(env)
		eph, nonce := string(raw[1:1+keySize]), string(raw[1+keySize:headerSize])
		if seen["e"+eph] || seen["n"+nonce] {
			t.Fatal("ephemeral key or nonce repeated across Seal calls")
		}
		seen["e"+eph], seen["n"+nonce] = true, true
	}
}

func TestOpenRejectsAllZeroShared(t *testing.T) {
	priv, pub, err := GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	env, err := Seal(pub, []byte("x"), LabelAnswer, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(env)
	for _, h := range lowOrderPoints {
		p, _ := hex.DecodeString(h)
		forged := append([]byte(nil), raw...)
		copy(forged[1:], p)
		if _, err := Open(priv, base64.RawURLEncoding.EncodeToString(forged), LabelAnswer, nil); err != ErrOpen {
			t.Errorf("low-order ephemeral %s accepted: %v", h[:8], err)
		}
	}
}

func TestSealRejectsLowOrderRecipient(t *testing.T) {
	for _, h := range lowOrderPoints {
		var pub [32]byte
		p, _ := hex.DecodeString(h)
		copy(pub[:], p)
		if env, err := Seal(pub, []byte("x"), LabelPush, nil); !errors.Is(err, ErrBadKey) || env != "" {
			t.Errorf("low-order recipient %s: env %q err %v", h[:8], env, err)
		}
	}
}

func TestEmptyLabelRejected(t *testing.T) {
	priv, pub, _ := GenerateX25519()
	if _, err := Seal(pub, nil, "", nil); err != ErrBadLabel {
		t.Fatalf("Seal with empty label: %v", err)
	}
	env, _ := Seal(pub, nil, LabelPush, nil)
	if _, err := Open(priv, env, "", nil); err != ErrOpen {
		t.Fatalf("Open with empty label: %v", err)
	}
}

// lowOrderPoints are X25519 encodings whose shared secret is all zero for any
// private key: 0, 1, the two order-8 points, p-1, p and p+1.
var lowOrderPoints = []string{
	"0000000000000000000000000000000000000000000000000000000000000000",
	"0100000000000000000000000000000000000000000000000000000000000000",
	"e0eb7a7c3b41b8ae1656e3faf19fc46ada098deb9c32b1fd866205165f49b800",
	"5f9c95bca3508c24b1d0b1559c83ef5b04445cc4581c8e86d8224eddd09f1157",
	"ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
	"edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
	"eeffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
}

func TestErrorsCarryNoKeyMaterial(t *testing.T) {
	priv, pub, _ := GenerateX25519()
	env, _ := Seal(pub, []byte("secret answer"), LabelAnswer, []byte(fixtureHostID))
	_, err := Open(priv, env, LabelAnswer, []byte("other"))
	for _, s := range []string{hex.EncodeToString(priv[:]), env, "secret answer"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error text leaks input: %q", err)
		}
	}
}

func TestOpenGarbageNoPanic(t *testing.T) {
	priv, _, _ := GenerateX25519()
	buf := make([]byte, 200)
	for i := range 500 {
		n := i % len(buf)
		_, _ = rand.Read(buf[:n])
		if n > 0 {
			buf[0] = Version
		}
		inputs := []string{base64.RawURLEncoding.EncodeToString(buf[:n]), string(buf[:n])}
		for _, in := range inputs {
			if _, err := Open(priv, in, LabelPush, nil); err != ErrOpen {
				t.Fatalf("random input opened: %v", err)
			}
		}
	}
}

func TestVerifyRejectsBadLengths(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	msg := SignMessage(fixtureHostID, "r_abcdefghij", "approve", 1)
	sig := Sign(priv, msg)
	if !Verify(pub, msg, sig) {
		t.Fatal("valid signature rejected")
	}
	if Verify(pub[:31], msg, sig) || Verify(pub, msg, sig[:63]) || Verify(nil, msg, nil) {
		t.Fatal("wrong-length key or signature accepted")
	}
}
