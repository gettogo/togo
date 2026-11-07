package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/doctor"
	"github.com/gettogo/togo/helper/internal/hooks"
)

// anyBinary counts every harness program as installed, so only the config
// roots in the temp HOME decide.
func anyBinary(t *testing.T) {
	t.Helper()
	old := binaryCheck
	binaryCheck = func(context.Context, string) func(string) bool { return func(string) bool { return true } }
	t.Cleanup(func() { binaryCheck = old })
}

type hooksHome struct {
	root string
	env  map[string]string
}

func newHooksHome(t *testing.T, dirs ...string) hooksHome {
	t.Helper()
	anyBinary(t)
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("TOGO_HOME", filepath.Join(root, "togo"))
	return hooksHome{root: root, env: map[string]string{}}
}

func (h hooksHome) run(args ...string) (int, string) {
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), args, h.env, strings.NewReader(""), &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestHooksCommandInstallStatusUninstall(t *testing.T) {
	h := newHooksHome(t, ".claude")
	if code, out := h.run("hooks", "install", "--agent", "claude"); code != 0 || !strings.Contains(out, "claude: installed") {
		t.Fatalf("install: code=%d out=%q", code, out)
	}
	want := `{"auggie":"no-agent","claude":"installed","codex":"no-agent","copilot":"no-agent","droid":"no-agent","gemini":"no-agent","qwen":"no-agent"}`
	if code, out := h.run("hooks", "status", "--json"); code != 0 || strings.TrimSpace(out) != want {
		t.Fatalf("status: code=%d out=%q", code, out)
	}
	if code, out := h.run("hooks", "uninstall"); code != 0 || !strings.Contains(out, "codex: skipped: not installed") {
		t.Fatalf("uninstall: code=%d out=%q", code, out)
	}
	if _, out := h.run("hooks", "status", "--json"); !strings.Contains(out, `"claude":"absent"`) {
		t.Fatalf("status after uninstall: %q", out)
	}
	if code, _ := h.run("hooks", "install", "--agent", "codex", "--local"); code != exitUsage {
		t.Errorf("--local with codex: code=%d", code)
	}
	if code, _ := h.run("hooks", "install", "--agent", "kimi"); code != exitUsage {
		t.Errorf("kimi takes no hooks: code=%d", code)
	}
}

// One plain install sets up every installed harness and says so, one line
// each; a second run writes nothing; uninstall puts every file back.
func TestHooksInstallAllHarnesses(t *testing.T) {
	h := newHooksHome(t, ".claude", ".codex", ".gemini", ".augment", ".qwen", ".factory", ".copilot")
	settings := filepath.Join(h.root, ".claude", "settings.json")
	orig := "{\n  \"model\": \"opus\",\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"bash ~/.claude/line.sh\"\n  }\n}\n"
	if err := os.WriteFile(settings, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := h.run("hooks", "install")
	if code != 0 {
		t.Fatalf("install: code=%d out=%q", code, out)
	}
	for _, a := range hooks.Agents {
		if !strings.Contains(out, a+": installed") {
			t.Errorf("no installed line for %s in %q", a, out)
		}
	}
	if !strings.Contains(out, "backup "+settings+".togo-backup") {
		t.Errorf("claude's backup not named: %q", out)
	}
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), "statusline --then 'bash ~/.claude/line.sh'") {
		t.Fatalf("status line not chained: %s", b)
	}
	if _, out := h.run("hooks", "install"); strings.Contains(out, "installed") {
		t.Fatalf("second install changed something: %q", out)
	}
	if code, out := h.run("hooks", "uninstall"); code != 0 {
		t.Fatalf("uninstall: %d %q", code, out)
	}
	if b, _ := os.ReadFile(settings); string(b) != orig {
		t.Fatalf("settings not restored:\n%s", b)
	}
	for _, p := range []string{".codex/hooks.json", ".gemini/settings.json", ".augment/settings.json", ".qwen/settings.json", ".factory/hooks.json", ".copilot/hooks/togo.json"} {
		if _, err := os.Stat(filepath.Join(h.root, p)); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestHooksInstallSkipsMissingHarnesses(t *testing.T) {
	h := newHooksHome(t, ".claude")
	binaryCheck = func(context.Context, string) func(string) bool {
		return func(c string) bool { return c != "claude" }
	}
	code, out := h.run("hooks", "install")
	if code != 0 || !strings.Contains(out, "claude: skipped: not installed") || !strings.Contains(out, "gemini: skipped: not installed") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestHooksNoStatusLineLeavesItAlone(t *testing.T) {
	h := newHooksHome(t, ".claude")
	settings := filepath.Join(h.root, ".claude", "settings.json")
	orig := `{"statusLine": {"type": "command", "command": "mine.sh"}}`
	if err := os.WriteFile(settings, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	h.run("hooks", "install", "--agent", "claude", "--no-statusline")
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), `"command": "mine.sh"`) {
		t.Fatalf("status line changed: %s", b)
	}
}

func TestHooksRefusesEditedEntry(t *testing.T) {
	h := newHooksHome(t, ".gemini")
	path := filepath.Join(h.root, ".gemini", "settings.json")
	edited := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "FOO=1 togo hook gemini SessionStart"}]}]}}`
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := h.run("hooks", "install", "--agent", "gemini")
	if code == 0 || !strings.Contains(out, "gemini: refused:") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if b, _ := os.ReadFile(path); string(b) != edited {
		t.Fatalf("edited file rewritten: %s", b)
	}
}

func TestDoctorPrintsIntegrations(t *testing.T) {
	old := doctorIntegrations
	t.Cleanup(func() { doctorIntegrations = old })
	doctorIntegrations = func(doctor.Env) []hooks.Report {
		return []hooks.Report{
			{ID: "claude", Name: "Claude Code", Installed: true, Setup: hooks.SetupHooks, State: hooks.StateReady,
				Hooks: "installed", StatusLine: hooks.LineChained, LastHookAt: "2026-11-07T10:00:00Z"},
			{ID: "gemini", Name: "Gemini CLI", Installed: true, Setup: hooks.SetupHooks, State: hooks.StateNeedsSetup,
				Hooks: "absent", Fix: "togo hooks install --agent gemini"},
			{ID: "kimi", Name: "Kimi Code", Installed: true, Setup: hooks.SetupNone, State: hooks.StateNotNeeded},
			{ID: "aider", Name: "Aider", Setup: hooks.SetupNone, State: hooks.StateNotInstalled},
		}
	}
	var buf bytes.Buffer
	runDoctor(nil, doctorEnv(t, "tmux 3.6a"), &buf)
	out := buf.String()
	for _, want := range []string{
		"claude     ready             hooks installed; status line chained; last hook 2026-11-07T10:00:00Z",
		"gemini     needs setup       hooks absent; no hook received yet; fix: togo hooks install --agent gemini",
		"kimi       no setup needed",
		"not installed: aider",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	buf.Reset()
	runDoctor([]string{"--json"}, doctorEnv(t, "tmux 3.6a"), &buf)
	if !strings.Contains(buf.String(), `"integrations":[{"id":"claude"`) || !strings.Contains(buf.String(), `"state":"needsSetup"`) {
		t.Fatalf("json: %s", buf.String())
	}
}
