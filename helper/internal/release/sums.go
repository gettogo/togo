package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumsName is the checksum file's name.
const SumsName = "SHA256SUMS"

func validAssetName(n string) bool {
	return n != "" && !strings.HasPrefix(n, ".") && !strings.ContainsAny(n, "/\\\n\r")
}

// WriteSums writes SHA256SUMS in dir covering the named files, one
// "<hex>  <name>" line each, sorted by name.
func WriteSums(dir string, names []string) (string, error) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	for i, n := range sorted {
		if !validAssetName(n) {
			return "", fmt.Errorf("invalid asset name %q", n)
		}
		if i > 0 && sorted[i-1] == n {
			return "", fmt.Errorf("duplicate asset name %q", n)
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		b.WriteString(hex.EncodeToString(sum[:]) + "  " + n + "\n")
	}
	path := filepath.Join(dir, SumsName)
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// ParseSums reads a SHA256SUMS file. It rejects duplicate names, digests that
// are not 64 lowercase hex digits, and names that contain a slash or start
// with a dot.
func ParseSums(data []byte) (map[string][32]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty checksum list")
	}
	text := strings.TrimSuffix(string(data), "\n")
	out := map[string][32]byte{}
	for i, line := range strings.Split(text, "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || len(digest) != 64 {
			return nil, fmt.Errorf("line %d: want \"<sha256>  <name>\"", i+1)
		}
		for _, r := range digest {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return nil, fmt.Errorf("line %d: digest is not lowercase hex", i+1)
			}
		}
		if !validAssetName(name) || name == ".." {
			return nil, fmt.Errorf("line %d: invalid name %q", i+1, name)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("line %d: duplicate name %q", i+1, name)
		}
		var sum [32]byte
		if _, err := hex.Decode(sum[:], []byte(digest)); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out[name] = sum
	}
	return out, nil
}
