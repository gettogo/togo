//go:build release

package release

import "testing"

// Release builds run the tests with -tags release; they must not pin the test key.
func TestReleaseKeyIsNotTestKey(t *testing.T) {
	pub, err := ParsePublicKey(ReleaseKey)
	if err != nil {
		t.Fatalf("embedded release key: %v", err)
	}
	if IsTestKey(pub) {
		t.Fatalf("minisign.pub is the test key %s; pin the owner's release key", TestKeyID)
	}
}
