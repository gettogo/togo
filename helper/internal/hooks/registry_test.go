package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/agents"
)

// Every registry harness is an agent the helper runs, and every agent but
// the shell has a registry row, so setup never forgets one.
func TestRegistryCoversEveryAgent(t *testing.T) {
	for _, a := range agents.Registry {
		if a.ID == "shell" {
			continue
		}
		h, ok := Lookup(a.ID)
		if !ok {
			t.Errorf("no setup row for %s", a.ID)
			continue
		}
		if h.Command != a.Command {
			t.Errorf("%s command %q, agents registry %q", a.ID, h.Command, a.Command)
		}
	}
	for _, h := range Registry {
		if _, ok := agents.Lookup(h.ID); !ok {
			t.Errorf("%s is not an agent", h.ID)
		}
		if h.Setup == SetupHooks && (h.root == nil || h.config == nil || len(h.events) == 0) {
			t.Errorf("%s takes hooks but has no config or events", h.ID)
		}
	}
	want := []string{Claude, Codex, Gemini, Auggie, Qwen, Droid, Copilot}
	if !slices.Equal(Agents, want) {
		t.Fatalf("Agents = %v, want %v", Agents, want)
	}
	if Known("vibe") || Known("kimi") {
		t.Fatal("a harness without confirmed hooks is installable")
	}
}

func TestPresentNeedsProgramAndConfigRoot(t *testing.T) {
	root := t.TempDir()
	o := Options{GeminiDir: filepath.Join(root, ".gemini")}
	gemini, _ := Lookup(Gemini)
	kimi, _ := Lookup("kimi")
	if gemini.Present(o) {
		t.Fatal("present without its config root")
	}
	if err := os.MkdirAll(o.GeminiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !gemini.Present(o) {
		t.Fatal("not present with its root and no program check")
	}
	o.HasBinary = func(c string) bool { return c == "kimi" }
	if gemini.Present(o) {
		t.Fatal("present without its program")
	}
	if !kimi.Present(o) {
		t.Fatal("kimi on PATH is not present")
	}
	if !slices.Equal(Default(o), []string(nil)) {
		t.Fatalf("default %v", Default(o))
	}
	o.HasBinary = func(string) bool { return true }
	if got := Default(o); !slices.Equal(got, []string{Gemini}) {
		t.Fatalf("default %v", got)
	}
}

func TestDefaultOptionsFollowHarnessEnv(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/c", "CODEX_HOME": "relative", "GEMINI_CLI_HOME": "/g"}
	o := DefaultOptions(func(k string) string { return env[k] }, "/home/u", "/home/u/.togo", "/bin/togo")
	if o.ClaudeDir != "/c" || o.CodexDir != "/home/u/.codex" || o.GeminiDir != "/g/.gemini" ||
		o.AugmentDir != "/home/u/.augment" || o.CopilotDir != "/home/u/.copilot" || o.Binary != "/bin/togo" {
		t.Fatalf("%+v", o)
	}
}
