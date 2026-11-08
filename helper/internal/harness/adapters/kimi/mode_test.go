package kimi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestInitialMode(t *testing.T) {
	cases := []struct {
		argv []string
		plan bool
		want string
	}{
		{[]string{"kimi"}, false, "default"},
		{[]string{"kimi", "--continue"}, true, "plan"},
		{[]string{"kimi", "--yolo", "--continue"}, false, "yolo"},
		{[]string{"kimi", "--auto"}, false, "auto"},
		{[]string{"kimi", "--plan", "--continue"}, false, "plan"},
	}
	for _, c := range cases {
		if got := initialMode(c.argv, c.plan); got != c.want {
			t.Errorf("initialMode(%v, %v) = %q, want %q", c.argv, c.plan, got, c.want)
		}
	}
}

func TestDefaultPlanModeFromConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	old := configPath
	configPath = func() string { return p }
	defer func() { configPath = old }()
	if defaultPlanMode() {
		t.Fatal("missing config read as plan")
	}
	os.WriteFile(p, []byte("default_model = \"k3\"\ndefault_plan_mode = true # x\n[thinking]\n"), 0o600)
	if !defaultPlanMode() {
		t.Fatal("default_plan_mode = true not read")
	}
	os.WriteFile(p, []byte("default_plan_mode = false\n[x]\ndefault_plan_mode = true\n"), 0o600)
	if defaultPlanMode() {
		t.Fatal("read a nested key or false as plan")
	}
}

// The mode is known before any record: Kimi logs no set_mode for ask.
func TestWireModeKnownAtStart(t *testing.T) {
	old := configPath
	configPath = func() string { return filepath.Join(t.TempDir(), "none.toml") }
	defer func() { configPath = old }()
	m := NewWire(harness.Env{Argv: []string{"kimi"}})
	if got := m.State()["mode"]; got != "default" {
		t.Fatalf("mode at start = %q, want default", got)
	}
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"plan_mode.enter","time":1794079438000}`)})
	if got := m.State()["mode"]; got != "plan" {
		t.Fatalf("mode after plan = %q, want plan", got)
	}
}
