package state

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoginEnvReadsShellExports(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "fakesh")
	script := "#!/bin/sh\necho 'Welcome'\nexport MOONSHOT_API_KEY=k1\nexport PWD=/somewhere\neval \"$2\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	env, ok := loginEnv(context.Background(), shell)
	if !ok {
		t.Fatal("loginEnv failed")
	}
	if !slices.Contains(env, "MOONSHOT_API_KEY=k1") {
		t.Fatalf("env lacks the exported key: %v", env)
	}
	for _, kv := range env {
		if kv == "PWD=/somewhere" || kv == "Welcome" {
			t.Fatalf("env kept %q", kv)
		}
	}
}

func TestLoginEnvFailsWithoutShell(t *testing.T) {
	if env, ok := loginEnv(context.Background(), "/nonexistent/shell"); ok || env != nil {
		t.Fatalf("loginEnv = %v, %v; want nil, false", env, ok)
	}
}

func TestParseEnvKeepsMultilineValues(t *testing.T) {
	got := parseEnv([]byte("A=1\x00B=two\nlines\x00=bad\x00SHLVL=2\x00"))
	want := []string{"A=1", "B=two\nlines"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseEnv = %q; want %q", got, want)
	}
}
