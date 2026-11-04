package seal

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// -update rewrites protocol/fixtures/crypto from the fixed inputs below. The
// files are the contract with the iOS app, so regenerate only on purpose and
// check the diff.
var update = flag.Bool("update", false, "rewrite protocol/fixtures/crypto")

var vectorDir = filepath.Join("..", "..", "..", "protocol", "fixtures", "crypto")

type sealVector struct {
	Name          string `json:"name"`
	RecipientPriv string `json:"recipientPriv"`
	RecipientPub  string `json:"recipientPub"`
	EphPriv       string `json:"ephPriv"`
	EphPub        string `json:"ephPub"`
	Nonce         string `json:"nonce"`
	Label         string `json:"label"`
	Info          string `json:"info"`
	AAD           string `json:"aad"`
	Plaintext     string `json:"plaintext"`
	SharedSecret  string `json:"sharedSecret"`
	Salt          string `json:"salt"`
	Key           string `json:"key"`
	Ciphertext    string `json:"ciphertext"`
	Envelope      string `json:"envelope"`
}

type sealNegative struct {
	Name          string `json:"name"`
	RecipientPriv string `json:"recipientPriv"`
	Label         string `json:"label"`
	AAD           string `json:"aad"`
	Envelope      string `json:"envelope"`
	Expect        string `json:"expect"`
}

type sealFile struct {
	Description string         `json:"description"`
	Vectors     []sealVector   `json:"vectors"`
	Negative    []sealNegative `json:"negative"`
}

type signVector struct {
	Name      string `json:"name"`
	Seed      string `json:"seed"`
	PublicKey string `json:"publicKey"`
	HostID    string `json:"hostId"`
	RequestID string `json:"requestId"`
	Decision  string `json:"decision"`
	TS        int64  `json:"ts"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
	Expect    string `json:"expect"`
}

type signFile struct {
	Description string       `json:"description"`
	Vectors     []signVector `json:"vectors"`
}

type hostIDVector struct {
	Name          string `json:"name"`
	MailboxSecret string `json:"mailboxSecret"`
	SHA256        string `json:"sha256"`
	HostID        string `json:"hostId"`
}

type hostKeyVector struct {
	Name    string `json:"name"`
	Priv    string `json:"x25519Priv"`
	Pub     string `json:"x25519Pub"`
	PubB64u string `json:"x25519PubBase64url"`
}

type hostIDFile struct {
	Description string          `json:"description"`
	HostIDs     []hostIDVector  `json:"hostIds"`
	Keys        []hostKeyVector `json:"x25519Keys"`
}

const fixtureHostID = "h_mmg42klgyqzwneiskrelxms3j7"

// seq returns 32 bytes start, start+1, ... as used by the push.pair fixture.
func seq(start byte) [32]byte {
	var b [32]byte
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

// testBytes derives fixed test-only randomness from a name, so the inputs are
// reproducible without storing a generator seed anywhere else.
func testBytes(name string) [32]byte { return sha256.Sum256([]byte("togo test vector: " + name)) }

func testNonce(name string) [12]byte {
	var n [12]byte
	b := testBytes(name + " nonce")
	copy(n[:], b[:])
	return n
}

type sealInput struct {
	name, label, aad, plaintext string
	recipientPriv               [32]byte
}

var hostPriv, devicePriv = seq(0x20), seq(0x40)

var sealInputs = []sealInput{
	{"push needs_you to device", LabelPush, fixtureHostID + "|dev_test-1",
		`{"v":1,"kind":"needs_you","sessionId":"s_abcdefghij","bib":"01","agent":"claude","title":"Run npm test?","body":"Bash: npm test","requestId":"r_abcdefghij","options":[{"n":1,"label":"Yes","role":"approve"},{"n":2,"label":"Yes, and don't ask again","role":"approve_always"},{"n":3,"label":"No","role":"deny"}],"ts":1793707200000}`,
		devicePriv},
	{"answer to host", LabelAnswer, fixtureHostID,
		`{"requestId":"r_abcdefghij","decision":"option:2","ts":1793707200000,"deviceId":"dev_test-1","sig":"lrFs0GYx8BssHKOJx69wiOsWATauhVSt4mml0s-xyoSGr8iqL5dML-AEaEKf9WmP9qLsXYO5eAljTts-s5SIAQ"}`,
		hostPriv},
	{"empty plaintext and aad", LabelPush, "", "", devicePriv},
}

func buildSealFile(t *testing.T) sealFile {
	t.Helper()
	f := sealFile{Description: sealDescription}
	for _, in := range sealInputs {
		ephPriv, nonce := testBytes(in.name+" ephemeral"), testNonce(in.name)
		rpub := mustPub(t, in.recipientPriv)
		epub := mustPub(t, ephPriv)
		env, err := sealWith(ephPriv, nonce, rpub, []byte(in.plaintext), in.label, []byte(in.aad))
		if err != nil {
			t.Fatalf("%s: %v", in.name, err)
		}
		eph, _ := ecdh.X25519().NewPrivateKey(ephPriv[:])
		shared, err := sharedSecret(eph, rpub)
		if err != nil {
			t.Fatal(err)
		}
		key, err := deriveKey(shared, epub, rpub, in.label)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(env)
		f.Vectors = append(f.Vectors, sealVector{
			Name: in.name, RecipientPriv: hx(in.recipientPriv[:]), RecipientPub: hx(rpub[:]),
			EphPriv: hx(ephPriv[:]), EphPub: hx(epub[:]), Nonce: hx(nonce[:]),
			Label: in.label, Info: infoPrefix + in.label, AAD: in.aad, Plaintext: in.plaintext,
			SharedSecret: hx(shared), Salt: hx(append(epub[:], rpub[:]...)), Key: hx(key),
			Ciphertext: hx(raw[headerSize:]), Envelope: env,
		})
	}
	f.Negative = buildNegatives(t, f.Vectors[0])
	return f
}

func buildNegatives(t *testing.T, v sealVector) []sealNegative {
	t.Helper()
	raw, _ := base64.RawURLEncoding.DecodeString(v.Envelope)
	enc := base64.RawURLEncoding.EncodeToString
	mut := func(f func(b []byte) []byte) string {
		return enc(f(append([]byte(nil), raw...)))
	}
	neg := func(name, env, label, aad, priv string) sealNegative {
		return sealNegative{name, priv, label, aad, env, "fail"}
	}
	otherPriv := testBytes("unrelated recipient")
	order8, _ := hex.DecodeString("e0eb7a7c3b41b8ae1656e3faf19fc46ada098deb9c32b1fd866205165f49b800")
	withEph := func(pub []byte) string {
		return mut(func(b []byte) []byte { copy(b[1:1+keySize], pub); return b })
	}
	p, l, a := v.RecipientPriv, v.Label, v.AAD
	return []sealNegative{
		neg("flipped ciphertext byte", mut(func(b []byte) []byte { b[headerSize] ^= 0x01; return b }), l, a, p),
		neg("flipped tag byte", mut(func(b []byte) []byte { b[len(b)-1] ^= 0x80; return b }), l, a, p),
		neg("flipped nonce byte", mut(func(b []byte) []byte { b[1+keySize] ^= 0x01; return b }), l, a, p),
		neg("wrong aad", v.Envelope, l, a+"x", p),
		neg("wrong label", v.Envelope, LabelAnswer, a, p),
		neg("wrong recipient", v.Envelope, l, a, hx(otherPriv[:])),
		neg("version byte 0x02", mut(func(b []byte) []byte { b[0] = 0x02; return b }), l, a, p),
		neg("short envelope", enc(raw[:headerSize+tagSize-1]), l, a, p),
		neg("empty envelope", "", l, a, p),
		neg("low-order ephemeral key zero", withEph(make([]byte, 32)), l, a, p),
		neg("low-order ephemeral key order 8", withEph(order8), l, a, p),
		neg("padded base64", v.Envelope+"=", l, a, p),
		neg("standard base64 alphabet", base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(raw), l, a, p),
	}
}

func buildSignFile(t *testing.T) signFile {
	t.Helper()
	f := signFile{Description: signDescription}
	add := func(name string, seed [32]byte, req, dec string, ts int64) signVector {
		priv := ed25519.NewKeyFromSeed(seed[:])
		msg := SignMessage(fixtureHostID, req, dec, ts)
		v := signVector{Name: name, Seed: hx(seed[:]),
			PublicKey: b64(priv.Public().(ed25519.PublicKey)), HostID: fixtureHostID,
			RequestID: req, Decision: dec, TS: ts, Message: string(msg),
			Signature: b64(Sign(priv, msg)), Expect: "ok"}
		f.Vectors = append(f.Vectors, v)
		return v
	}
	var s11 [32]byte
	copy(s11[:], bytes.Repeat([]byte{0x11}, 32))
	base := add("answer fixture option:2", s11, "r_abcdefghij", "option:2", 1793707200000)
	add("approve", testBytes("signer approve"), "r_234567abcd", "approve", 1793707260123)
	add("deny", testBytes("signer deny"), "r_zzzzzzzzzz", "deny", 1)

	bad := func(name string, edit func(v *signVector)) {
		v := base
		v.Name, v.Expect = name, "fail"
		edit(&v)
		v.Message = string(SignMessage(v.HostID, v.RequestID, v.Decision, v.TS))
		f.Vectors = append(f.Vectors, v)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(base.Signature)
	bad("other decision", func(v *signVector) { v.Decision = "option:3" })
	bad("other ts", func(v *signVector) { v.TS++ })
	bad("other host", func(v *signVector) { v.HostID = "h_aaaaaaaaaaaaaaaaaaaaaaaaaa" })
	bad("other key", func(v *signVector) {
		other := testBytes("signer approve")
		v.PublicKey = b64(ed25519.NewKeyFromSeed(other[:]).Public().(ed25519.PublicKey))
	})
	bad("flipped signature bit", func(v *signVector) {
		s := append([]byte(nil), sig...)
		s[0] ^= 0x01
		v.Signature = b64(s)
	})
	bad("short signature", func(v *signVector) { v.Signature = b64(sig[:63]) })
	bad("non-canonical s", func(v *signVector) { v.Signature = b64(addOrder(sig)) })
	return f
}

// addOrder returns sig with L added to its scalar half. The result verifies
// under a malleable implementation, so it must be rejected.
func addOrder(sig []byte) []byte {
	l := []byte{0xed, 0xd3, 0xf5, 0x5c, 0x1a, 0x63, 0x12, 0x58, 0xd6, 0x9c, 0xf7, 0xa2, 0xde, 0xf9, 0xde, 0x14,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10}
	out := append([]byte(nil), sig...)
	carry := 0
	for i := 0; i < 32; i++ {
		s := int(out[32+i]) + int(l[i]) + carry
		out[32+i], carry = byte(s), s>>8
	}
	return out
}

func buildHostIDFile(t *testing.T) hostIDFile {
	t.Helper()
	f := hostIDFile{Description: hostIDDescription}
	secrets := []struct {
		name   string
		secret []byte
	}{
		{"push.pair fixture mailbox secret 0x00..0x1f", func() []byte { s := seq(0x00); return s[:] }()},
		{"all 0xff", bytes.Repeat([]byte{0xff}, 32)},
	}
	for _, s := range secrets {
		sum := sha256.Sum256(s.secret)
		f.HostIDs = append(f.HostIDs, hostIDVector{s.name, hx(s.secret), hx(sum[:]), HostID(s.secret)})
	}
	for _, k := range []struct {
		name string
		priv [32]byte
	}{{"push.pair fixture host key 0x20..0x3f", hostPriv}, {"push.pair fixture device key 0x40..0x5f", devicePriv}} {
		pub := mustPub(t, k.priv)
		f.Keys = append(f.Keys, hostKeyVector{k.name, hx(k.priv[:]), hx(pub[:]), b64(pub[:])})
	}
	return f
}

func hx(b []byte) string  { return hex.EncodeToString(b) }
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustPub(t *testing.T, priv [32]byte) [32]byte {
	t.Helper()
	pub, err := PublicKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// checkFile compares want against the committed file, or rewrites it with -update.
func checkFile(t *testing.T, name string, want any) {
	t.Helper()
	path := filepath.Join(vectorDir, name)
	data, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *update {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("%s differs from the vectors this package produces; rerun with -update only if the change is intended", name)
	}
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vectorDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func unhex32(t *testing.T, s string) [32]byte {
	t.Helper()
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		t.Fatalf("bad 32-byte hex %q", s)
	}
	copy(out[:], b)
	return out
}

func TestVectorsSeal(t *testing.T) {
	checkFile(t, "seal.json", buildSealFile(t))
	var f sealFile
	readJSON(t, "seal.json", &f)
	if len(f.Vectors) == 0 {
		t.Fatal("no seal vectors")
	}
	for _, v := range f.Vectors {
		var nonce [12]byte
		nb, _ := hex.DecodeString(v.Nonce)
		copy(nonce[:], nb)
		priv, eph := unhex32(t, v.RecipientPriv), unhex32(t, v.EphPriv)
		if pub := mustPub(t, priv); hx(pub[:]) != v.RecipientPub {
			t.Errorf("%s: recipientPub mismatch", v.Name)
		}
		env, err := sealWith(eph, nonce, unhex32(t, v.RecipientPub), []byte(v.Plaintext), v.Label, []byte(v.AAD))
		if err != nil || env != v.Envelope {
			t.Errorf("%s: sealWith does not reproduce the envelope (err %v)", v.Name, err)
		}
		pt, err := Open(priv, v.Envelope, v.Label, []byte(v.AAD))
		if err != nil || string(pt) != v.Plaintext {
			t.Errorf("%s: Open failed (err %v)", v.Name, err)
		}
	}
}

func TestVectorsNegative(t *testing.T) {
	var f sealFile
	readJSON(t, "seal.json", &f)
	if len(f.Negative) == 0 {
		t.Fatal("no negative vectors")
	}
	for _, v := range f.Negative {
		pt, err := Open(unhex32(t, v.RecipientPriv), v.Envelope, v.Label, []byte(v.AAD))
		if err != ErrOpen || pt != nil || v.Expect != "fail" {
			t.Errorf("%s: want ErrOpen, got err %v", v.Name, err)
		}
	}
}

func TestSignVectors(t *testing.T) {
	checkFile(t, "sign.json", buildSignFile(t))
	var f signFile
	readJSON(t, "sign.json", &f)
	for _, v := range f.Vectors {
		pub, _ := base64.RawURLEncoding.DecodeString(v.PublicKey)
		sig, _ := base64.RawURLEncoding.DecodeString(v.Signature)
		msg := SignMessage(v.HostID, v.RequestID, v.Decision, v.TS)
		if string(msg) != v.Message {
			t.Errorf("%s: message mismatch", v.Name)
		}
		if got := Verify(pub, msg, sig); got != (v.Expect == "ok") {
			t.Errorf("%s: Verify = %v, want expect %q", v.Name, got, v.Expect)
		}
	}
}

func TestHostIDVector(t *testing.T) {
	checkFile(t, "hostid.json", buildHostIDFile(t))
	var f hostIDFile
	readJSON(t, "hostid.json", &f)
	re := regexp.MustCompile(`^h_[a-z2-7]{26}$`)
	for _, v := range f.HostIDs {
		secret, _ := hex.DecodeString(v.MailboxSecret)
		if got := HostID(secret); got != v.HostID || !re.MatchString(got) {
			t.Errorf("%s: HostID = %q, want %q", v.Name, got, v.HostID)
		}
	}
	if f.HostIDs[0].HostID != fixtureHostID {
		t.Errorf("fixture host id = %q, want %q", f.HostIDs[0].HostID, fixtureHostID)
	}
	if f.Keys[0].PubB64u != "NYBy1jZYgNGu6jKa35EhODhR7SGijjt16WXQ0s0WYlQ" ||
		f.Keys[1].PubB64u != "eaYx7t4b-cmPEgMs3q3Q56B5OY_HhriMyEbsia-FpRo" {
		t.Error("x25519 keys do not match the push.pair fixture")
	}
}

const sealDescription = "Sealed envelope vectors. All keys are test-only values: recipient keys are the push.pair fixture host key (bytes 0x20..0x3f) and device key (bytes 0x40..0x5f); ephPriv and nonce are SHA-256(\"togo test vector: \" + name + \" ephemeral\") and the first 12 bytes of SHA-256(\"togo test vector: \" + name + \" nonce\"). " +
	"Encoding: keys, nonce, sharedSecret, salt, key and ciphertext are lowercase hex; label, info, aad and plaintext are UTF-8 strings; envelope is base64url without padding. " +
	"Steps: sharedSecret = X25519(ephPriv, recipientPub), rejected when all zero; salt = ephPub || recipientPub; key = HKDF-SHA256(ikm=sharedSecret, salt, info=\"togo/v1/\" + label, L=32); ciphertext = ChaCha20-Poly1305(key, nonce, plaintext, aad), ciphertext then 16-byte tag; envelope = 0x01 || ephPub || nonce || ciphertext. " +
	"vectors: sealing with ephPriv and nonce must reproduce envelope byte for byte, and opening with recipientPriv must return plaintext. " +
	"negative: opening envelope with recipientPriv, label and aad must fail without revealing which check failed (bad base64url, version not 0x01, shorter than 61 bytes, low-order ephemeral key, wrong label, aad or key, tampered bytes)."

const signDescription = "Approval signature vectors. Ed25519 over the UTF-8 bytes of message = \"togo-approval/v1\\n\" + hostId + \"\\n\" + requestId + \"\\n\" + decision + \"\\n\" + ts, ts in decimal Unix milliseconds. " +
	"seed is the 32-byte Ed25519 private seed in hex (test-only: 0x11 repeated for the answer fixture, otherwise SHA-256(\"togo test vector: signer ...\")); publicKey and signature are base64url without padding. " +
	"expect ok: the signature verifies under publicKey; signers with deterministic Ed25519 reproduce it exactly, hedged signers (CryptoKit) need only verify it. " +
	"expect fail: verification must return false. Fields other than the edited one are copied from the first vector; message is rebuilt from the edited fields. non-canonical s adds the group order L to the scalar half and must be rejected."

const hostIDDescription = "Host identity vectors. hostIds: hostId = \"h_\" + the first 26 characters of lowercase RFC 4648 base32 (no padding) of SHA-256(mailboxSecret); mailboxSecret and sha256 are hex. " +
	"x25519Keys: X25519 public key of x25519Priv (hex), given in hex and base64url without padding; these are the push.pair fixture keys. All values are test-only."
