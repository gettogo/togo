package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/release"
	"golang.org/x/crypto/blake2b"
)

const fixtures = "../../internal/release/testdata"

// releaseDir copies the signed fixture sums into a temp dir and writes files
// whose hashes the fixture lists are not expected to match.
func releaseDir(t *testing.T, sig string) string {
	t.Helper()
	dir := t.TempDir()
	for src, dst := range map[string]string{"SHA256SUMS": "SHA256SUMS", sig: "SHA256SUMS.minisig"} {
		b, err := os.ReadFile(filepath.Join(fixtures, src))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, dst), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMain(m *testing.M) {
	// The fixtures are signed with the test key that is pinned until the
	// release key exists; TestVerifyRefusesPinnedTestKey covers the guard.
	os.Setenv("TOGO_RELEASE_TEST", "1")
	os.Exit(m.Run())
}

func TestVerifyRefusesPinnedTestKey(t *testing.T) {
	pinned, err := release.ParsePublicKey(release.ReleaseKey)
	if err != nil {
		t.Fatal(err)
	}
	if !release.IsTestKey(pinned) {
		t.Skip("pinned key is not the test key")
	}
	t.Setenv("TOGO_RELEASE_TEST", "")
	dir := releaseDir(t, "SHA256SUMS.minisig")
	for _, args := range [][]string{
		{"--version", "v1.0.0", "--signature-only", dir},
		{"--version", "v1.0.0", "--pub", filepath.Join(fixtures, "test.pub"), "--signature-only", dir},
	} {
		code, _, errs := runVerifyArgs(args...)
		if code != 1 || !strings.Contains(errs, "test key") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errs)
		}
	}
}

func runVerifyArgs(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(append([]string{"verify"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func TestVerifySignatureOnly(t *testing.T) {
	dir := releaseDir(t, "SHA256SUMS.minisig")
	code, out, errs := runVerifyArgs("--version", "v1.0.0", "--pub", filepath.Join(fixtures, "test.pub"), "--signature-only", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "v1.0.0") {
		t.Fatalf("stdout %q", out)
	}
}

func TestVerifyChecksFileHashes(t *testing.T) {
	dir := releaseDir(t, "SHA256SUMS.minisig")
	// The fixture hashes are not the hashes of any real file, so a full check fails.
	code, _, errs := runVerifyArgs("--version", "v1.0.0", "--pub", filepath.Join(fixtures, "test.pub"), dir)
	if code != 1 || !strings.Contains(errs, "togo_1.0.0_darwin_amd64.tar.gz") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
}

func TestVerifyCommandRejects(t *testing.T) {
	pub := filepath.Join(fixtures, "test.pub")
	cases := map[string]struct {
		sig, version, want string
	}{
		"other key":   {"other.minisig", "v1.0.0", "different key"},
		"old version": {"old-version.minisig", "v1.0.0", "different release"},
	}
	for name, c := range cases {
		dir := releaseDir(t, c.sig)
		code, _, errs := runVerifyArgs("--version", c.version, "--pub", pub, "--signature-only", dir)
		if code != 1 || !strings.Contains(errs, c.want) {
			t.Errorf("%s: exit %d, stderr %q", name, code, errs)
		}
	}

	dir := releaseDir(t, "SHA256SUMS.minisig")
	if err := os.Remove(filepath.Join(dir, "SHA256SUMS.minisig")); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runVerifyArgs("--version", "v1.0.0", "--pub", pub, "--signature-only", dir); code != 1 || !strings.Contains(errs, "SHA256SUMS.minisig") {
		t.Errorf("missing signature: exit %d, stderr %q", code, errs)
	}
}

func TestVerifyUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--version", "v1.0.0"}, {"somedir"}, {"--version", "v1.0.0", "a", "b"}} {
		if code, _, errs := runVerifyArgs(args...); code != 2 || !strings.Contains(errs, "usage") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errs)
		}
	}
}

func TestVerifyFullRelease(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"togo_1.2.3_linux_amd64.tar.gz": "archive", "install.sh": "#!/bin/sh\n"}
	var sums strings.Builder
	for _, name := range []string{"install.sh", "togo_1.2.3_linux_amd64.tar.gz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(files[name]))
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	pubPath := signTestRelease(t, dir, []byte(sums.String()), "togo v1.2.3 SHA256SUMS")

	if code, out, errs := runVerifyArgs("--version", "v1.2.3", "--pub", pubPath, dir); code != 0 || !strings.Contains(out, "2 files") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}

	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runVerifyArgs("--version", "v1.2.3", "--pub", pubPath, dir); code != 1 || !strings.Contains(errs, "install.sh") {
		t.Fatalf("modified file: exit %d, stderr %q", code, errs)
	}
}

// signTestRelease writes SHA256SUMS and a minisign signature made with a fresh
// key, and returns the path of the matching public key file.
func signTestRelease(t *testing.T, dir string, sums []byte, comment string) string {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := []byte("testkey1")
	digest := blake2b.Sum512(sums)
	sig := ed25519.Sign(sk, digest[:])
	global := ed25519.Sign(sk, append(append([]byte{}, sig...), comment...))
	enc := base64.StdEncoding.EncodeToString
	minisig := "untrusted comment: test\n" + enc(append(append([]byte("ED"), id...), sig...)) +
		"\ntrusted comment: " + comment + "\n" + enc(global) + "\n"
	pub := "untrusted comment: test\n" + enc(append(append([]byte("Ed"), id...), pk...)) + "\n"
	pubPath := filepath.Join(t.TempDir(), "test.pub")
	for path, data := range map[string]string{
		filepath.Join(dir, "SHA256SUMS"):         string(sums),
		filepath.Join(dir, "SHA256SUMS.minisig"): minisig,
		pubPath:                                  pub,
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return pubPath
}

func TestSumsEntriesRejectUnsafeNames(t *testing.T) {
	names, err := sumsEntries([]byte("0b1e4a4c6f1d2e3a5b7c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c  togo_1.0.0_linux_amd64.tar.gz\n"))
	if err != nil || len(names) != 1 {
		t.Fatalf("sumsEntries: %v %v", names, err)
	}
	const h = "0b1e4a4c6f1d2e3a5b7c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c"
	for _, bad := range []string{
		"zz  x\n",
		h + "  ../etc/passwd\n",
		h + "  sub/x\n",
		h + "  .hidden\n",
		h + " x\n",
		h + "  x\n" + h + "  x\n",
		"",
	} {
		if _, err := sumsEntries([]byte(bad)); err == nil {
			t.Errorf("sumsEntries(%q) accepted", bad)
		}
	}
}
