package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumsFormatSorted(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"b.tar.gz": "bee", "a.tar.gz": "ay", "install.sh": "sh"}
	names := []string{"b.tar.gz", "install.sh", "a.tar.gz"}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path, err := WriteSums(dir, names)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "SHA256SUMS" {
		t.Errorf("path = %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, n := range []string{"a.tar.gz", "b.tar.gz", "install.sh"} {
		sum := sha256.Sum256([]byte(files[n]))
		want.WriteString(hex.EncodeToString(sum[:]) + "  " + n + "\n")
	}
	if string(got) != want.String() {
		t.Errorf("SHA256SUMS =\n%s\nwant\n%s", got, want.String())
	}
	parsed, err := ParseSums(got)
	if err != nil || len(parsed) != 3 {
		t.Fatalf("ParseSums = %v, %v", parsed, err)
	}
	if parsed["a.tar.gz"] != sha256.Sum256([]byte("ay")) {
		t.Error("parsed digest mismatch")
	}
}

func TestParseSumsRejectsDuplicateAndPathNames(t *testing.T) {
	h := strings.Repeat("ab", 32)
	cases := map[string]string{
		"duplicate":   h + "  a\n" + h + "  a\n",
		"slash":       h + "  dir/a\n",
		"dotfile":     h + "  .hidden\n",
		"dotdot":      h + "  ..\n",
		"short hex":   "abcd  a\n",
		"non hex":     strings.Repeat("zz", 32) + "  a\n",
		"upper hex":   strings.Repeat("AB", 32) + "  a\n",
		"one space":   h + " a\n",
		"empty":       "",
		"blank line":  h + "  a\n\n",
		"empty name":  h + "  \n",
		"backslash":   h + "  a\\b\n",
		"binary flag": h + " *a\n",
	}
	for name, in := range cases {
		if _, err := ParseSums([]byte(in)); err == nil {
			t.Errorf("%s: ParseSums accepted %q", name, in)
		}
	}
	if _, err := ParseSums([]byte(h + "  a\n" + h + "  b\n")); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
}

func TestWriteSumsRejectsPathNames(t *testing.T) {
	if _, err := WriteSums(t.TempDir(), []string{"sub/a"}); err == nil {
		t.Error("accepted a path name")
	}
}
