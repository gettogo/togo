package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHooksCommandInstallStatusUninstall(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("TOGO_HOME", filepath.Join(root, "togo"))
	env := map[string]string{"CLAUDE_CONFIG_DIR": claude}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := runContext(context.Background(), args, env, strings.NewReader(""), &out, &errOut)
		return code, out.String() + errOut.String()
	}

	if code, out := run("hooks", "install", "--agent", "claude"); code != 0 || !strings.Contains(out, "claude: installed") {
		t.Fatalf("install: code=%d out=%q", code, out)
	}
	if code, out := run("hooks", "status", "--json"); code != 0 || strings.TrimSpace(out) != `{"claude":"installed","codex":"no-agent","copilot":"no-agent","droid":"no-agent","qwen":"no-agent"}` {
		t.Fatalf("status: code=%d out=%q", code, out)
	}
	if code, out := run("hooks", "uninstall"); code != 0 || !strings.Contains(out, "codex: not found") {
		t.Fatalf("uninstall: code=%d out=%q", code, out)
	}
	if _, out := run("hooks", "status", "--json"); strings.TrimSpace(out) != `{"claude":"absent","codex":"no-agent","copilot":"no-agent","droid":"no-agent","qwen":"no-agent"}` {
		t.Fatalf("status after uninstall: %q", out)
	}
	if code, _ := run("hooks", "install", "--agent", "codex", "--local"); code != exitUsage {
		t.Errorf("--local with codex: code=%d", code)
	}
	// qwen is written only when named, never by the default "all".
	if err := os.MkdirAll(filepath.Join(root, ".qwen"), 0o755); err != nil {
		t.Fatal(err)
	}
	run("hooks", "install")
	if _, err := os.Stat(filepath.Join(root, ".qwen", "settings.json")); err == nil {
		t.Fatal("install without --agent wrote qwen's settings")
	}
	if code, out := run("hooks", "install", "--agent", "qwen"); code != 0 || !strings.Contains(out, "qwen: installed") {
		t.Fatalf("qwen install: code=%d out=%q", code, out)
	}
	if code, _ := run("hooks", "install", "--agent", "qwen", "--local"); code != exitUsage {
		t.Errorf("--local with qwen: code=%d", code)
	}
}
