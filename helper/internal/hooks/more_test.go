package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func optionalEnv(t *testing.T) env {
	t.Helper()
	e := newEnv(t)
	root := filepath.Dir(e.o.ClaudeDir)
	e.o.FactoryDir, e.o.CopilotDir = filepath.Join(root, "factory"), filepath.Join(root, "copilot")
	for _, d := range []string{e.o.FactoryDir, e.o.CopilotDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// Droid and Copilot are part of the default set whenever they are
// installed.
func TestDefaultSetIncludesInstalledHookHarnesses(t *testing.T) {
	e := optionalEnv(t)
	for _, a := range []string{Droid, Copilot} {
		if !Known(a) {
			t.Fatalf("%s unknown", a)
		}
	}
	got := Default(e.o)
	for _, a := range []string{Claude, Codex, Droid, Copilot} {
		if !slices.Contains(got, a) {
			t.Fatalf("default %v lacks %s", got, a)
		}
	}
	mustOK(t, Install(e.o, Default(e.o)))
	for _, p := range []string{filepath.Join(e.o.FactoryDir, "hooks.json"), filepath.Join(e.o.CopilotDir, "hooks", "togo.json")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("default install did not write %s", p)
		}
	}
}

func TestDroidInstallKeepsHooksAndUninstallRestores(t *testing.T) {
	e := optionalEnv(t)
	path := filepath.Join(e.o.FactoryDir, "hooks.json")
	orig := "{\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"say done\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	write(t, path, []byte(orig))
	mustOK(t, Install(e.o, []string{Droid}))
	got := read(t, path)
	for _, want := range []string{`say done`, `hook droid SubagentStop`, `hook droid SessionEnd`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if st := Status(e.o)[Droid]; st != StatusInstalled {
		t.Fatalf("status %s", st)
	}
	mustOK(t, Install(e.o, []string{Droid}))
	if read(t, path) != got {
		t.Fatal("a second install changed the file")
	}
	mustOK(t, Uninstall(e.o, []string{Droid}))
	if read(t, path) != orig {
		t.Fatalf("uninstall left %s", read(t, path))
	}
}

func TestDroidAndCopilotInstall(t *testing.T) {
	e := optionalEnv(t)
	e.o.Block = true
	mustOK(t, Install(e.o, []string{Droid, Copilot}))
	droid := read(t, filepath.Join(e.o.FactoryDir, "hooks.json"))
	if !strings.Contains(droid, "hook droid PreToolUse") || strings.Contains(droid, "PermissionRequest") {
		t.Fatalf("droid %s", droid)
	}
	cp := filepath.Join(e.o.CopilotDir, "hooks", "togo.json")
	var doc struct {
		Version int `json:"version"`
		Hooks   map[string][]struct {
			Type, Bash string
			TimeoutSec int
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(read(t, cp)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Hooks["preToolUse"][0].Bash != hookCommand("/opt/togo/bin/togo", Copilot, "preToolUse") ||
		doc.Hooks["permissionRequest"][0].TimeoutSec != 600 {
		t.Fatalf("copilot %+v", doc)
	}
	st := Status(e.o)
	if st[Droid] != StatusInstalled || st[Copilot] != StatusInstalled {
		t.Fatalf("status %v", st)
	}
	mustOK(t, Uninstall(e.o, []string{Droid, Copilot}))
	if _, err := os.Stat(cp); err == nil {
		t.Fatal("copilot hook file left behind")
	}
	if _, err := os.Stat(filepath.Join(e.o.FactoryDir, "hooks.json")); err == nil {
		t.Fatal("droid hook file togo created was left behind")
	}
}

func TestOptionalAgentMissingIsSkipped(t *testing.T) {
	e := newEnv(t)
	e.o.FactoryDir = filepath.Join(t.TempDir(), "absent")
	rs := Install(e.o, []string{Droid, Copilot})
	for _, r := range rs {
		if r.State != "skipped" || r.Err != nil {
			t.Fatalf("%+v", r)
		}
	}
}
