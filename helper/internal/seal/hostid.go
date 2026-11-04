package seal

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

var lowerBase32 = base32.NewEncoding(strings.ToLower("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")).WithPadding(base32.NoPadding)

// HostID derives the public host id from the mailbox secret:
// "h_" + lowercase base32 (no padding) of SHA-256(secret), first 26 chars.
// The relay recomputes it from the Bearer secret, so the id proves knowledge
// of the secret without revealing it.
func HostID(secret []byte) string {
	sum := sha256.Sum256(secret)
	return "h_" + lowerBase32.EncodeToString(sum[:])[:26]
}
