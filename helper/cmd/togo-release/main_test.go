package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/release"
)

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"build"}, {"sums"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code == 0 {
			t.Errorf("run(%v) = 0, want nonzero", args)
		}
		if !strings.Contains(errb.String(), "usage:") {
			t.Errorf("run(%v) stderr = %q, want usage", args, errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage:") {
		t.Errorf("help = %d %q", code, out.String())
	}
}

func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("MIT"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := "module example.com/m\n\ngo 1.26.0\n\ntoolchain go1.26.10\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func fakeGo(t *testing.T, log, goVersion string) string {
	t.Helper()
	fake := filepath.Join(t.TempDir(), "go")
	script := `#!/bin/sh
if [ "$1" = env ]; then echo ` + goVersion + `; exit 0; fi
echo "$GOOS $GOARCH cgo=$CGO_ENABLED goflags=[$GOFLAGS] toolchain=$GOTOOLCHAIN sde=$SOURCE_DATE_EPOCH $*" >> ` + log + `
while [ $# -gt 0 ]; do
  if [ "$1" = -o ]; then out="$2"; fi
  shift
done
mkdir -p "$(dirname "$out")"
echo "bin $GOOS $GOARCH" > "$out"
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestBuildWritesAllTargets(t *testing.T) {
	repo := fixtureRepo(t)
	log := filepath.Join(t.TempDir(), "argv.log")
	fake := fakeGo(t, log, "go1.26.10")
	dist := filepath.Join(t.TempDir(), "dist")
	var out, errb bytes.Buffer
	code := run([]string{"build", "--version", "v1.2.3", "--out", dist, "--module-dir", repo, "--go", fake}, &out, &errb)
	if code != 0 {
		t.Fatalf("build = %d, stderr: %s", code, errb.String())
	}
	rawLog, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(rawLog)), "\n")
	if len(lines) != len(release.Targets) {
		t.Fatalf("go ran %d times, want %d:\n%s", len(lines), len(release.Targets), rawLog)
	}
	epoch := testGit(t, repo, "log", "-1", "--format=%ct")
	for i, tg := range release.Targets {
		l := lines[i]
		for _, want := range []string{
			tg.OS + " " + tg.Arch + " cgo=0 goflags=[] toolchain=go1.26.10 sde=" + epoch,
			" build ", "-trimpath", "-buildvcs=false", "-mod=readonly",
			"-ldflags -s -w -buildid= -X github.com/gettogo/togo/helper/internal/version.Version=v1.2.3",
			"./cmd/togo",
		} {
			if !strings.Contains(l, want) {
				t.Errorf("line %q lacks %q", l, want)
			}
		}
		name := "togo_1.2.3_" + tg.OS + "_" + tg.Arch + ".tar.gz"
		if _, err := os.Stat(filepath.Join(dist, name)); err != nil {
			t.Errorf("missing archive: %v", err)
		}
	}
	info, err := os.ReadFile(filepath.Join(dist, "BUILDINFO.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version": "v1.2.3"`, `"goVersion": "go1.26.10"`, `"linux/arm64"`, `"darwin/amd64"`, testGit(t, repo, "rev-parse", "HEAD")} {
		if !strings.Contains(string(info), want) {
			t.Errorf("BUILDINFO.json lacks %q:\n%s", want, info)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "SHA256SUMS")); err == nil {
		t.Error("build must not write SHA256SUMS")
	}

	// sums covers every asset except itself and signatures.
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS.minisig"), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"sums", dist}, &out, &errb); code != 0 {
		t.Fatalf("sums = %d, stderr: %s", code, errb.String())
	}
	raw, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := release.ParseSums(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 5 {
		t.Errorf("sums lists %d files, want 4 archives + BUILDINFO.json: %s", len(parsed), raw)
	}
	if _, ok := parsed["SHA256SUMS.minisig"]; ok {
		t.Error("sums lists the signature")
	}
	if _, ok := parsed["BUILDINFO.json"]; !ok {
		t.Error("sums misses BUILDINFO.json")
	}
	// Running again must not list the previous SHA256SUMS.
	if code := run([]string{"sums", dist}, &out, &errb); code != 0 {
		t.Fatalf("second sums = %d", code)
	}
	raw2, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if !bytes.Equal(raw, raw2) {
		t.Error("sums is not idempotent")
	}
}

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestBuildRejectsToolchainMismatch(t *testing.T) {
	repo := fixtureRepo(t)
	log := filepath.Join(t.TempDir(), "argv.log")
	fake := fakeGo(t, log, "go1.26.0")
	var out, errb bytes.Buffer
	code := run([]string{"build", "--version", "v1.2.3", "--out", t.TempDir(), "--module-dir", repo, "--go", fake}, &out, &errb)
	if code == 0 || !strings.Contains(errb.String(), "go.mod pins go1.26.10") {
		t.Fatalf("code = %d, stderr = %q", code, errb.String())
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("go build ran despite the toolchain mismatch")
	}
}

func TestBuildRefusesDirtyTreeAndMarksDevBuilds(t *testing.T) {
	repo := fixtureRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "LICENSE"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := fakeGo(t, filepath.Join(t.TempDir(), "argv.log"), "go1.26.10")
	dist := filepath.Join(t.TempDir(), "dist")
	var out, errb bytes.Buffer
	base := []string{"build", "--version", "v1.2.3", "--out", dist, "--module-dir", repo, "--go", fake}
	if code := run(base, &out, &errb); code == 0 {
		t.Fatal("dirty tree built without --allow-dirty")
	}
	if _, err := os.Stat(dist); err == nil {
		t.Error("refused build wrote output")
	}
	if code := run(append(base, "--allow-dirty"), &out, &errb); code != 0 {
		t.Fatalf("allow-dirty build = %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(dist, "BUILDINFO.json")); err == nil {
		t.Error("dev build wrote into the release directory")
	}
	info, err := os.ReadFile(filepath.Join(dist, "dev", "BUILDINFO.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"dirty": true`, `"version": "v1.2.3+dirty"`} {
		if !strings.Contains(string(info), want) {
			t.Errorf("BUILDINFO.json lacks %s:\n%s", want, info)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "dev", "togo_1.2.3+dirty_linux_amd64.tar.gz")); err != nil {
		t.Error(err)
	}
}

func TestArchivePacksGivenBinary(t *testing.T) {
	repo := fixtureRepo(t)
	bin := filepath.Join(t.TempDir(), "togo")
	if err := os.WriteFile(bin, []byte("signed"), 0o755); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	var out, errb bytes.Buffer
	args := []string{"archive", "--version", "v1.2.3", "--target", "darwin/arm64", "--binary", bin, "--out", dist, "--module-dir", repo}
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("archive = %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(dist, "togo_1.2.3_darwin_arm64.tar.gz")); err != nil {
		t.Error(err)
	}
	if code := run([]string{"archive", "--version", "v1.2.3"}, &out, &errb); code == 0 {
		t.Error("archive without --binary succeeded")
	}
}
