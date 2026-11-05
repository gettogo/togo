package release

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testVersion = "v1.0.0"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testKey(t *testing.T) PublicKey {
	t.Helper()
	pub, err := ParsePublicKey(string(readTestdata(t, "test.pub")))
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	return pub
}

// sigLines splits a .minisig file into its four lines.
func sigLines(t *testing.T, sig []byte) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(sig), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("signature file has %d lines", len(lines))
	}
	return lines
}

func joinSig(lines []string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func TestVerifySumsAcceptsTestRelease(t *testing.T) {
	err := VerifySums(testKey(t), readTestdata(t, "SHA256SUMS"), readTestdata(t, "SHA256SUMS.minisig"), testVersion)
	if err != nil {
		t.Fatalf("VerifySums: %v", err)
	}
}

func TestVerifyRejectsFlippedByte(t *testing.T) {
	pub := testKey(t)
	sums := readTestdata(t, "SHA256SUMS")
	sig := readTestdata(t, "SHA256SUMS.minisig")

	t.Run("sums", func(t *testing.T) {
		for _, i := range []int{0, len(sums) / 2, len(sums) - 1} {
			bad := append([]byte(nil), sums...)
			bad[i] ^= 0x01
			if err := VerifySums(pub, bad, sig, testVersion); !errors.Is(err, ErrBadSignature) {
				t.Fatalf("byte %d: got %v, want ErrBadSignature", i, err)
			}
		}
	})
	t.Run("truncated sums", func(t *testing.T) {
		if err := VerifySums(pub, sums[:len(sums)-10], sig, testVersion); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("got %v, want ErrBadSignature", err)
		}
	})
	t.Run("signature", func(t *testing.T) {
		lines := sigLines(t, sig)
		raw, err := base64.StdEncoding.DecodeString(lines[1])
		if err != nil {
			t.Fatal(err)
		}
		raw[len(raw)-1] ^= 0x01
		lines[1] = base64.StdEncoding.EncodeToString(raw)
		if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("got %v, want ErrBadSignature", err)
		}
	})
	t.Run("global signature", func(t *testing.T) {
		lines := sigLines(t, sig)
		raw, err := base64.StdEncoding.DecodeString(lines[3])
		if err != nil {
			t.Fatal(err)
		}
		raw[0] ^= 0x01
		lines[3] = base64.StdEncoding.EncodeToString(raw)
		if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("got %v, want ErrBadSignature", err)
		}
	})
}

func TestVerifyRejectsMalformedSignature(t *testing.T) {
	pub := testKey(t)
	sums := readTestdata(t, "SHA256SUMS")
	sig := readTestdata(t, "SHA256SUMS.minisig")
	lines := sigLines(t, sig)
	cases := map[string][]byte{
		"empty":            nil,
		"truncated":        sig[:len(sig)/2],
		"missing global":   joinSig(lines[:3]),
		"bad base64":       joinSig([]string{lines[0], "!!" + lines[1][2:], lines[2], lines[3]}),
		"short signature":  joinSig([]string{lines[0], lines[1][:40], lines[2], lines[3]}),
		"no untrusted":     joinSig([]string{"comment: x", lines[1], lines[2], lines[3]}),
		"no trusted":       joinSig([]string{lines[0], lines[1], strings.TrimPrefix(lines[2], "trusted comment: "), lines[3]}),
		"extra line":       joinSig(append(append([]string(nil), lines...), "junk")),
		"short global sig": joinSig([]string{lines[0], lines[1], lines[2], lines[3][:20]}),
	}
	for name, bad := range cases {
		if err := VerifySums(pub, sums, bad, testVersion); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: got %v, want ErrMalformed", name, err)
		}
	}
}

func TestVerifyRejectsOtherKey(t *testing.T) {
	err := VerifySums(testKey(t), readTestdata(t, "SHA256SUMS"), readTestdata(t, "other.minisig"), testVersion)
	if !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestVerifyRejectsOtherKeyWithSameID(t *testing.T) {
	// A forged signature that copies the pinned key ID still fails the Ed25519 check.
	pub := testKey(t)
	lines := sigLines(t, readTestdata(t, "other.minisig"))
	raw, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	copy(raw[2:10], pub.ID[:])
	lines[1] = base64.StdEncoding.EncodeToString(raw)
	err = VerifySums(pub, readTestdata(t, "SHA256SUMS"), joinSig(lines), testVersion)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v, want ErrBadSignature", err)
	}
}

func TestVerifyRejectsOlderVersionComment(t *testing.T) {
	err := VerifySums(testKey(t), readTestdata(t, "SHA256SUMS"), readTestdata(t, "old-version.minisig"), testVersion)
	if !errors.Is(err, ErrWrongVersion) {
		t.Fatalf("got %v, want ErrWrongVersion", err)
	}
	// The same signature is valid for the version it names.
	if err := VerifySums(testKey(t), readTestdata(t, "SHA256SUMS"), readTestdata(t, "old-version.minisig"), "v0.9.0"); err != nil {
		t.Fatalf("v0.9.0: %v", err)
	}
}

func TestVerifyRejectsWrongOrEmptyVersion(t *testing.T) {
	sums, sig := readTestdata(t, "SHA256SUMS"), readTestdata(t, "SHA256SUMS.minisig")
	for _, v := range []string{"", "v1.0.1", "1.0.0", "v1.0.0 ", "v1.0"} {
		if err := VerifySums(testKey(t), sums, sig, v); !errors.Is(err, ErrWrongVersion) {
			t.Errorf("%q: got %v, want ErrWrongVersion", v, err)
		}
	}
}

func TestVerifyRejectsTamperedTrustedComment(t *testing.T) {
	pub := testKey(t)
	sums := readTestdata(t, "SHA256SUMS")
	// Relabel the old signature as the current version: the global signature no longer matches.
	lines := sigLines(t, readTestdata(t, "old-version.minisig"))
	lines[2] = "trusted comment: togo v1.0.0 SHA256SUMS"
	if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("relabelled: got %v, want ErrBadSignature", err)
	}
	lines = sigLines(t, readTestdata(t, "SHA256SUMS.minisig"))
	lines[2] += " "
	if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("trailing space: got %v, want ErrBadSignature", err)
	}
}

func TestVerifyRejectsUnknownAlgorithm(t *testing.T) {
	pub := testKey(t)
	sums := readTestdata(t, "SHA256SUMS")
	for _, alg := range []string{"XX", "ed", "EE", "\x00\x00"} {
		lines := sigLines(t, readTestdata(t, "SHA256SUMS.minisig"))
		raw, err := base64.StdEncoding.DecodeString(lines[1])
		if err != nil {
			t.Fatal(err)
		}
		copy(raw[:2], alg)
		lines[1] = base64.StdEncoding.EncodeToString(raw)
		if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: got %v, want ErrMalformed", alg, err)
		}
	}
	// The legacy algorithm signs the raw message, so a prehashed signature relabelled "Ed" fails.
	lines := sigLines(t, readTestdata(t, "SHA256SUMS.minisig"))
	raw, _ := base64.StdEncoding.DecodeString(lines[1])
	copy(raw[:2], "Ed")
	lines[1] = base64.StdEncoding.EncodeToString(raw)
	if err := VerifySums(pub, sums, joinSig(lines), testVersion); !errors.Is(err, ErrBadSignature) {
		t.Errorf("relabelled Ed: got %v, want ErrBadSignature", err)
	}
}

func TestParsePublicKeyFileAndLine(t *testing.T) {
	file := string(readTestdata(t, "test.pub"))
	fromFile, err := ParsePublicKey(file)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	line := strings.Split(strings.TrimSpace(file), "\n")[1]
	fromLine, err := ParsePublicKey(line)
	if err != nil {
		t.Fatalf("line: %v", err)
	}
	if fromFile.ID != fromLine.ID || !fromFile.Key.Equal(fromLine.Key) {
		t.Fatal("file and line parse differently")
	}
	if len(fromFile.Key) != 32 {
		t.Fatalf("key length %d", len(fromFile.Key))
	}
	if _, err := ParsePublicKey(strings.ReplaceAll(file, "\n", "\r\n")); err != nil {
		t.Fatalf("CRLF file: %v", err)
	}

	raw, _ := base64.StdEncoding.DecodeString(line)
	badAlg := append([]byte("ED"), raw[2:]...)
	for name, bad := range map[string]string{
		"empty":       "",
		"comment":     "untrusted comment: minisign public key\n",
		"base64":      "RWQ!!!!",
		"short":       base64.StdEncoding.EncodeToString(raw[:20]),
		"long":        base64.StdEncoding.EncodeToString(append(raw, 0)),
		"algorithm":   base64.StdEncoding.EncodeToString(badAlg),
		"three lines": file + line + "\n",
	} {
		if _, err := ParsePublicKey(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: got %v, want ErrMalformed", name, err)
		}
	}
}

func TestReleaseKeyParses(t *testing.T) {
	if _, err := ParsePublicKey(ReleaseKey); err != nil {
		t.Fatalf("embedded release key: %v", err)
	}
}

func TestTestKeyIsRecognised(t *testing.T) {
	pub := testKey(t)
	if got := KeyIDString(pub.ID); got != TestKeyID {
		t.Fatalf("test key ID %s, want %s", got, TestKeyID)
	}
	if !IsTestKey(pub) {
		t.Fatal("IsTestKey(test key) = false")
	}
	other, err := ParsePublicKey("RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3")
	if err != nil {
		t.Fatal(err)
	}
	if IsTestKey(other) {
		t.Fatal("IsTestKey(other key) = true")
	}
}

func TestCheckPinnedKey(t *testing.T) {
	pinned, err := ParsePublicKey(ReleaseKey)
	if err != nil {
		t.Fatal(err)
	}
	if !IsTestKey(pinned) {
		t.Skip("pinned key is not the test key")
	}
	if err := CheckPinnedKey(func(string) string { return "" }); !errors.Is(err, ErrTestKeyPinned) {
		t.Fatalf("without TOGO_RELEASE_TEST: got %v, want ErrTestKeyPinned", err)
	}
	for _, v := range []string{"0", "true", " 1"} {
		if err := CheckPinnedKey(func(string) string { return v }); !errors.Is(err, ErrTestKeyPinned) {
			t.Fatalf("TOGO_RELEASE_TEST=%q: got %v, want ErrTestKeyPinned", v, err)
		}
	}
	env := func(k string) string {
		if k == "TOGO_RELEASE_TEST" {
			return "1"
		}
		return ""
	}
	if err := CheckPinnedKey(env); err != nil {
		t.Fatalf("TOGO_RELEASE_TEST=1: %v", err)
	}
}
