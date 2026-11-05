package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const formulaTemplate = "../../../scripts/release/togo.rb.tmpl"

// sumsFor builds a SHA256SUMS body listing the archives for version 1.2.3,
// each with a distinct, recognisable hash.
func sumsFor(skip string) string {
	var b strings.Builder
	for i, target := range []string{"darwin_arm64", "darwin_amd64", "linux_amd64", "linux_arm64"} {
		name := "togo_1.2.3_" + target + ".tar.gz"
		if target == skip {
			continue
		}
		b.WriteString(strings.Repeat(string(rune('a'+i)), 64) + "  " + name + "\n")
	}
	b.WriteString(strings.Repeat("f", 64) + "  install.sh\n")
	return b.String()
}

func writeSums(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "SHA256SUMS")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFormulaRendersAllTargets(t *testing.T) {
	sums := writeSums(t, sumsFor(""))
	var out, errb bytes.Buffer
	code := run([]string{"formula", "--version", "v1.2.3", "--sums", sums,
		"--base-url", "https://example.com/releases/download/v1.2.3/", "--template", formulaTemplate}, &out, &errb)
	if code != 0 {
		t.Fatalf("formula = %d, stderr %q", code, errb.String())
	}
	golden := filepath.Join("testdata", "togo.rb.golden")
	if *updateGolden {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("rendered formula differs from %s:\n%s", golden, out.String())
	}
	if ruby, err := exec.LookPath("ruby"); err == nil {
		path := filepath.Join(t.TempDir(), "togo.rb")
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if b, err := exec.Command(ruby, "-c", path).CombinedOutput(); err != nil {
			t.Errorf("ruby -c: %v\n%s", err, b)
		}
	}
}

func TestFormulaRejectsMissingTarget(t *testing.T) {
	sums := writeSums(t, sumsFor("linux_arm64"))
	var out, errb bytes.Buffer
	code := run([]string{"formula", "--version", "v1.2.3", "--sums", sums,
		"--base-url", "https://example.com/r", "--template", formulaTemplate}, &out, &errb)
	if code == 0 {
		t.Fatal("formula succeeded with a missing target")
	}
	if !strings.Contains(errb.String(), "togo_1.2.3_linux_arm64.tar.gz") {
		t.Errorf("stderr %q does not name the missing archive", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("wrote partial output %q", out.String())
	}
}

func TestFormulaRejectsBadInput(t *testing.T) {
	sums := writeSums(t, sumsFor(""))
	for _, args := range [][]string{
		{"formula", "--version", "1.2.3", "--sums", sums, "--base-url", "https://x.test"},
		{"formula", "--version", "v1.2.3", "--sums", sums, "--base-url", "http://x.test"},
		{"formula", "--version", "v1.2.3", "--sums", sums},
	} {
		var out, errb bytes.Buffer
		if code := run(append(args, "--template", formulaTemplate), &out, &errb); code == 0 {
			t.Errorf("run(%v) = 0, want nonzero", args)
		}
	}
}
