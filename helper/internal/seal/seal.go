// Package seal implements the end-to-end crypto shared by the helper and the
// phone: sealed envelopes for pushes and mailbox answers (X25519, HKDF-SHA256,
// ChaCha20-Poly1305), Ed25519 approval signatures, and the host id derived
// from the mailbox secret. The byte layouts are frozen by the vectors in
// protocol/fixtures/crypto, which the iOS app checks against CryptoKit.
package seal

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

// Labels select the HKDF info string, so a push envelope can never be opened
// as an answer or the other way round.
const (
	LabelPush   = "push"
	LabelAnswer = "answer"
)

// Version is the first byte of every envelope.
const Version byte = 0x01

const (
	keySize    = 32
	nonceSize  = chacha20poly1305.NonceSize
	tagSize    = chacha20poly1305.Overhead
	headerSize = 1 + keySize + nonceSize
	infoPrefix = "togo/v1/"
)

// ErrOpen is the only error Open returns. Callers learn that an envelope was
// rejected but not why, which keeps Open from acting as a decryption oracle.
var ErrOpen = errors.New("seal: envelope rejected")

// ErrBadKey reports a recipient public key that is low order or otherwise
// unusable for X25519.
var ErrBadKey = errors.New("seal: unusable recipient key")

// ErrBadLabel reports an empty label.
var ErrBadLabel = errors.New("seal: empty label")

// GenerateX25519 returns a new X25519 key pair from crypto/rand.
func GenerateX25519() (priv, pub [32]byte, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return priv, pub, err
	}
	copy(priv[:], k.Bytes())
	copy(pub[:], k.PublicKey().Bytes())
	return priv, pub, nil
}

// PublicKey returns the X25519 public key for priv.
func PublicKey(priv [32]byte) ([32]byte, error) {
	var pub [32]byte
	k, err := ecdh.X25519().NewPrivateKey(priv[:])
	if err != nil {
		return pub, err
	}
	copy(pub[:], k.PublicKey().Bytes())
	return pub, nil
}

// Seal encrypts plaintext to recipientPub and returns the base64url envelope.
// Every call uses a fresh ephemeral key, so each message has its own AEAD key
// and the random nonce is never paired with a key twice.
func Seal(recipientPub [32]byte, plaintext []byte, label string, aad []byte) (string, error) {
	var ephPriv [32]byte
	var nonce [nonceSize]byte
	if _, err := rand.Read(ephPriv[:]); err != nil {
		return "", err
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return sealWith(ephPriv, nonce, recipientPub, plaintext, label, aad)
}

// sealWith is Seal with the randomness supplied, so tests can reproduce the
// published vectors byte for byte. Production code must not call it.
func sealWith(ephPriv [32]byte, nonce [nonceSize]byte, recipientPub [32]byte, plaintext []byte, label string, aad []byte) (string, error) {
	if label == "" {
		return "", ErrBadLabel
	}
	eph, err := ecdh.X25519().NewPrivateKey(ephPriv[:])
	if err != nil {
		return "", err
	}
	var ephPub [32]byte
	copy(ephPub[:], eph.PublicKey().Bytes())
	key, err := messageKey(eph, recipientPub, ephPub, recipientPub, label)
	if err != nil {
		return "", ErrBadKey
	}
	aead, err := chacha20poly1305.New(key)
	clear(key)
	if err != nil {
		return "", err
	}
	out := make([]byte, headerSize, headerSize+len(plaintext)+tagSize)
	out[0] = Version
	copy(out[1:], ephPub[:])
	copy(out[1+keySize:], nonce[:])
	out = aead.Seal(out, nonce[:], plaintext, aad)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Open decrypts an envelope addressed to priv. It returns ErrOpen for every
// failure: bad encoding, wrong version, short input, low-order ephemeral key,
// wrong label or aad, and tampered ciphertext.
func Open(priv [32]byte, envelope string, label string, aad []byte) ([]byte, error) {
	if label == "" {
		return nil, ErrOpen
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(envelope)
	if err != nil || len(raw) < headerSize+tagSize || raw[0] != Version {
		return nil, ErrOpen
	}
	self, err := ecdh.X25519().NewPrivateKey(priv[:])
	if err != nil {
		return nil, ErrOpen
	}
	var ephPub, selfPub [32]byte
	copy(ephPub[:], raw[1:1+keySize])
	copy(selfPub[:], self.PublicKey().Bytes())
	key, err := messageKey(self, ephPub, ephPub, selfPub, label)
	if err != nil {
		return nil, ErrOpen
	}
	aead, err := chacha20poly1305.New(key)
	clear(key)
	if err != nil {
		return nil, ErrOpen
	}
	nonce := raw[1+keySize : headerSize]
	pt, err := aead.Open(nil, nonce, raw[headerSize:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

// messageKey computes X25519(self, peer) and feeds it to HKDF with the salt
// ephPub || recipientPub, which both sides know in the same order.
func messageKey(self *ecdh.PrivateKey, peer, ephPub, recipientPub [32]byte, label string) ([]byte, error) {
	shared, err := sharedSecret(self, peer)
	if err != nil {
		return nil, err
	}
	defer clear(shared)
	return deriveKey(shared, ephPub, recipientPub, label)
}

// sharedSecret runs X25519 and rejects the all-zero output that a low-order
// peer point produces. crypto/ecdh already refuses it; the explicit check
// keeps that guarantee if the library behaviour ever changes.
func sharedSecret(self *ecdh.PrivateKey, peer [32]byte) ([]byte, error) {
	pk, err := ecdh.X25519().NewPublicKey(peer[:])
	if err != nil {
		return nil, err
	}
	shared, err := self.ECDH(pk)
	if err != nil {
		return nil, err
	}
	var zero [32]byte
	if subtle.ConstantTimeCompare(shared, zero[:]) == 1 {
		return nil, ErrBadKey
	}
	return shared, nil
}

// deriveKey is HKDF-SHA256(ikm=shared, salt=ephPub||recipientPub,
// info="togo/v1/"+label, L=32).
func deriveKey(shared []byte, ephPub, recipientPub [32]byte, label string) ([]byte, error) {
	salt := make([]byte, 0, 2*keySize)
	salt = append(salt, ephPub[:]...)
	salt = append(salt, recipientPub[:]...)
	return hkdf.Key(sha256.New, shared, salt, infoPrefix+label, keySize)
}
