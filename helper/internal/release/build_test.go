package release

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init")
	return dir
}

func TestBuildRefusesDirtyTree(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	fake := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := Config{Version: "v1.0.0", ModuleDir: dir, OutDir: t.TempDir(), GoBin: fake}
	_, err := Build(context.Background(), c, Target{"linux", "amd64"})
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v, want dirty tree refusal", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("go ran despite the dirty tree")
	}
}

func TestBuildIsReproducible(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the helper twice")
	}
	var sums [][]byte
	for i := 0; i < 2; i++ {
		t.Setenv("GOCACHE", t.TempDir())
		c := Config{
			Version: "v0.0.0-test", Commit: "deadbeef", ModuleDir: "../..", OutDir: t.TempDir(),
			SourceDateEpoch: 1700000000, AllowDirty: true,
		}
		path, err := Build(context.Background(), c, Target{"linux", "amd64"})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sums = append(sums, data)
	}
	if !bytes.Equal(sums[0], sums[1]) {
		t.Error("two builds into different directories differ")
	}
}

func TestToolchainReadsPin(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("module m\n\ngo 1.26.0\n\ntoolchain go1.26.10\n")
	if got, err := Toolchain(dir); err != nil || got != "go1.26.10" {
		t.Errorf("Toolchain = %q, %v", got, err)
	}
	write("module m\n\ngo 1.26.0\n")
	if _, err := Toolchain(dir); err == nil {
		t.Error("accepted a go.mod without a toolchain line")
	}
}
