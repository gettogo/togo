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
	if code, out := run("hooks", "status", "--json"); code != 0 || strings.TrimSpace(out) != `{"claude":"installed","codex":"no-agent"}` {
		t.Fatalf("status: code=%d out=%q", code, out)
	}
	if code, out := run("hooks", "uninstall"); code != 0 || !strings.Contains(out, "codex: not found") {
		t.Fatalf("uninstall: code=%d out=%q", code, out)
	}
	if _, out := run("hooks", "status", "--json"); strings.TrimSpace(out) != `{"claude":"absent","codex":"no-agent"}` {
		t.Fatalf("status after uninstall: %q", out)
	}
	if code, _ := run("hooks", "install", "--agent", "codex", "--local"); code != exitUsage {
		t.Errorf("--local with codex: code=%d", code)
	}
}
