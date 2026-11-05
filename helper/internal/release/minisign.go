package release

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// ReleaseKey is the minisign public key that signs every SHA256SUMS.
//
//go:embed minisign.pub
var ReleaseKey string

var (
	ErrBadSignature  = errors.New("minisign: signature does not verify")
	ErrWrongKey      = errors.New("minisign: signed by a different key")
	ErrWrongVersion  = errors.New("minisign: trusted comment names a different release")
	ErrMalformed     = errors.New("minisign: malformed key or signature")
	ErrTestKeyPinned = errors.New("minisign: the pinned release key is the test key; set TOGO_RELEASE_TEST=1 only for tests")
)

// TestKeyID is the ID of the key that signs testdata/. Its secret key must not
// be kept; it stays pinned only until the owner's release key replaces it.
const TestKeyID = "9AA09F4C20C5559E"

// KeyIDString formats a key ID the way minisign prints it: the eight bytes
// as a little-endian integer in upper-case hex.
func KeyIDString(id [8]byte) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 16)
	for i := len(id) - 1; i >= 0; i-- {
		out = append(out, digits[id[i]>>4], digits[id[i]&0x0f])
	}
	return string(out)
}

// IsTestKey reports whether pub is the test key.
func IsTestKey(pub PublicKey) bool { return KeyIDString(pub.ID) == TestKeyID }

// CheckPinnedKey refuses to go on while ReleaseKey is the test key, unless
// TOGO_RELEASE_TEST is exactly "1". getenv is os.Getenv outside tests.
func CheckPinnedKey(getenv func(string) string) error {
	pub, err := ParsePublicKey(ReleaseKey)
	if err != nil {
		return err
	}
	if IsTestKey(pub) && getenv("TOGO_RELEASE_TEST") != "1" {
		return ErrTestKeyPinned
	}
	return nil
}

// PublicKey is a minisign Ed25519 public key and its key ID.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

const (
	untrustedPrefix = "untrusted comment: "
	trustedPrefix   = "trusted comment: "
	algLegacy       = "Ed" // signs the message itself
	algPrehashed    = "ED" // signs BLAKE2b-512 of the message
)

// ParsePublicKey reads a minisign .pub file or just its base64 line.
func ParsePublicKey(text string) (PublicKey, error) {
	lines := splitLines(text)
	switch {
	case len(lines) == 2 && strings.HasPrefix(lines[0], untrustedPrefix):
		lines = lines[1:]
	case len(lines) != 1:
		return PublicKey{}, fmt.Errorf("%w: public key must be one base64 line after an optional comment", ErrMalformed)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(lines[0])
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize {
		return PublicKey{}, fmt.Errorf("%w: public key is not 42 bytes of base64", ErrMalformed)
	}
	if string(raw[:2]) != algLegacy {
		return PublicKey{}, fmt.Errorf("%w: public key algorithm %q", ErrMalformed, raw[:2])
	}
	var pub PublicKey
	copy(pub.ID[:], raw[2:10])
	pub.Key = ed25519.PublicKey(bytes.Clone(raw[10:]))
	return pub, nil
}

// VerifySums checks a minisign signature over a SHA256SUMS file. It verifies the
// key ID, the signature over the file, the global signature over the trusted
// comment, and that the comment is exactly "togo <wantVersion> SHA256SUMS".
// Any other outcome is an error; there is no partial success.
func VerifySums(pub PublicKey, sums, sig []byte, wantVersion string) error {
	if len(pub.Key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: public key is empty", ErrMalformed)
	}
	s, err := parseSignature(sig)
	if err != nil {
		return err
	}
	if s.keyID != pub.ID {
		return ErrWrongKey
	}
	msg := sums
	if s.alg == algPrehashed {
		sum := blake2b.Sum512(sums)
		msg = sum[:]
	}
	if !ed25519.Verify(pub.Key, msg, s.sig[:]) {
		return ErrBadSignature
	}
	global := append(append(make([]byte, 0, len(s.sig)+len(s.trusted)), s.sig[:]...), s.trusted...)
	if !ed25519.Verify(pub.Key, global, s.globalSig) {
		return ErrBadSignature
	}
	if wantVersion == "" || s.trusted != "togo "+wantVersion+" SHA256SUMS" {
		return fmt.Errorf("%w: got %q, want version %q", ErrWrongVersion, s.trusted, wantVersion)
	}
	return nil
}

type signature struct {
	alg       string
	keyID     [8]byte
	sig       [ed25519.SignatureSize]byte
	trusted   string
	globalSig []byte
}

func parseSignature(data []byte) (signature, error) {
	var s signature
	lines := splitLines(string(data))
	if len(lines) != 4 || !strings.HasPrefix(lines[0], untrustedPrefix) || !strings.HasPrefix(lines[2], trustedPrefix) {
		return s, fmt.Errorf("%w: signature file must have four lines", ErrMalformed)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return s, fmt.Errorf("%w: signature is not 74 bytes of base64", ErrMalformed)
	}
	s.alg = string(raw[:2])
	if s.alg != algPrehashed && s.alg != algLegacy {
		return s, fmt.Errorf("%w: signature algorithm %q", ErrMalformed, raw[:2])
	}
	copy(s.keyID[:], raw[2:10])
	copy(s.sig[:], raw[10:])
	s.trusted = strings.TrimPrefix(lines[2], trustedPrefix)
	s.globalSig, err = base64.StdEncoding.Strict().DecodeString(lines[3])
	if err != nil || len(s.globalSig) != ed25519.SignatureSize {
		return s, fmt.Errorf("%w: global signature is not 64 bytes of base64", ErrMalformed)
	}
	return s, nil
}

// splitLines splits on LF, drops a CR before each LF, and ignores one final
// newline. Blank lines elsewhere are kept so they fail the line count.
func splitLines(text string) []string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
