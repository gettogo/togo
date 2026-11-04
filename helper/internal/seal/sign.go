package seal

import (
	"crypto/ed25519"

	"github.com/gettogo/togo/helper/internal/wire"
)

// SignMessage returns the bytes a device signs to answer an approval. It is
// wire.ApprovalSignedBytes, re-exported so callers of this package never build
// the string themselves.
func SignMessage(hostID, requestID, decision string, tsMillis int64) []byte {
	return wire.ApprovalSignedBytes(hostID, requestID, decision, tsMillis)
}

// Sign signs msg with priv. The helper never signs answers; this exists for
// tests and tooling that play the device side.
func Sign(priv ed25519.PrivateKey, msg []byte) []byte {
	return ed25519.Sign(priv, msg)
}

// Verify reports whether sig is a valid Ed25519 signature of msg by pub.
// Keys and signatures of the wrong length are rejected instead of panicking.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}
