package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/release"
)

const verifyUsage = "usage: togo-release verify --version vX.Y.Z [--pub file] [--signature-only] <dir>\n"

// runVerify checks <dir>/SHA256SUMS.minisig against the pinned release key (or
// --pub), then that every file SHA256SUMS lists is present with that hash.
func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "release version, e.g. v1.0.0")
	pubPath := fs.String("pub", "", "minisign public key file (default: the pinned release key)")
	sigOnly := fs.Bool("signature-only", false, "check the signature but not the listed files")
	if err := fs.Parse(args); err != nil || *version == "" || fs.NArg() != 1 {
		fmt.Fprint(stderr, verifyUsage)
		return 2
	}
	dir := fs.Arg(0)

	n, err := verifyRelease(dir, *version, *pubPath, *sigOnly)
	if err != nil {
		fmt.Fprintf(stderr, "togo-release verify: %v\n", err)
		return 1
	}
	if *sigOnly {
		fmt.Fprintf(stdout, "SHA256SUMS signature is valid for %s\n", *version)
	} else {
		fmt.Fprintf(stdout, "SHA256SUMS signature is valid for %s; %d files match\n", *version, n)
	}
	return 0
}

func verifyRelease(dir, version, pubPath string, sigOnly bool) (int, error) {
	if err := release.CheckPinnedKey(os.Getenv); err != nil {
		return 0, err
	}
	keyText := release.ReleaseKey
	if pubPath != "" {
		b, err := os.ReadFile(pubPath)
		if err != nil {
			return 0, err
		}
		keyText = string(b)
	}
	pub, err := release.ParsePublicKey(keyText)
	if err != nil {
		return 0, err
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return 0, err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.minisig"))
	if err != nil {
		return 0, err
	}
	if err := release.VerifySums(pub, sums, sig, version); err != nil {
		return 0, err
	}
	if sigOnly {
		return 0, nil
	}
	entries, err := sumsEntries(sums)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.name))
		if err != nil {
			return 0, err
		}
		got := sha256.Sum256(data)
		if !bytes.Equal(got[:], e.sum) {
			return 0, fmt.Errorf("%s does not match SHA256SUMS", e.name)
		}
	}
	return len(entries), nil
}

type sumsEntry struct {
	sum  []byte
	name string
}

// sumsEntries parses "<sha256 hex>  <name>" lines and refuses anything that
// could name a file outside the release directory.
func sumsEntries(data []byte) ([]sumsEntry, error) {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, errors.New("SHA256SUMS is empty")
	}
	seen := map[string]bool{}
	var out []sumsEntry
	for _, line := range strings.Split(text, "\n") {
		hexSum, name, ok := strings.Cut(line, "  ")
		sum, err := hex.DecodeString(hexSum)
		if !ok || err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("SHA256SUMS line %q is malformed", line)
		}
		if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") || strings.TrimSpace(name) != name {
			return nil, fmt.Errorf("SHA256SUMS names an unsafe path %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("SHA256SUMS lists %q twice", name)
		}
		seen[name] = true
		out = append(out, sumsEntry{sum, name})
	}
	return out, nil
}
