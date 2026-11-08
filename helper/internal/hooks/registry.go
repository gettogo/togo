package hooks

import (
	"os"
	"path/filepath"
	"slices"
)

// Setup says what a harness needs from Togo's setup step.
type Setup string

// Setup kinds.
const (
	// SetupHooks: Togo writes hook entries into the harness's config.
	SetupHooks Setup = "hooks"
	// SetupNone: the harness talks to Togo another way (its own wire
	// protocol, a server, an rpc mode or the screen), so nothing is written.
	SetupNone Setup = "none"
	// SetupPending: the harness has hooks, but their dialect is not
	// confirmed against a real install yet, so Togo writes nothing.
	SetupPending Setup = "pending"
)

// Harness is one row of the setup registry.
type Harness struct {
	ID   string
	Name string
	// Command is the program found on PATH when the harness is installed.
	Command string
	Setup   Setup
	// StatusLine is true when Togo chains the harness's status line.
	StatusLine bool
	// root is the harness's config directory; config is the file Togo
	// edits (empty for SetupNone).
	root   func(Options) string
	config func(Options) string
	// events are the hook events Togo installs (without --block).
	events []string
}

// Root is the harness's config directory, empty when unknown.
func (h Harness) Root(o Options) string {
	if h.root == nil {
		return ""
	}
	return h.root(o)
}

// Config is the file Togo edits for the harness, empty when none.
func (h Harness) Config(o Options) string {
	if h.config == nil {
		return ""
	}
	return h.config(o)
}

func in(dir func(Options) string, file ...string) func(Options) string {
	return func(o Options) string {
		d := dir(o)
		if d == "" {
			return ""
		}
		return filepath.Join(append([]string{d}, file...)...)
	}
}

var (
	claudeDir  = func(o Options) string { return o.ClaudeDir }
	codexDir   = func(o Options) string { return o.CodexDir }
	augmentDir = func(o Options) string { return o.AugmentDir }
	factoryDir = func(o Options) string { return o.FactoryDir }
	copilotDir = func(o Options) string { return o.CopilotDir }
	vibeDir    = func(o Options) string { return o.VibeDir }
)

// Registry lists every harness Togo runs, in the order setup reports them.
// Ids and commands match the agents registry.
var Registry = []Harness{
	{ID: Claude, Name: "Claude Code", Command: "claude", Setup: SetupHooks, StatusLine: true,
		root: claudeDir, config: in(claudeDir, "settings.json"), events: claudeBase},
	{ID: Codex, Name: "Codex", Command: "codex", Setup: SetupHooks,
		root: codexDir, config: in(codexDir, "hooks.json"), events: codexEvents},
	{ID: Auggie, Name: "Auggie", Command: "auggie", Setup: SetupHooks,
		root: augmentDir, config: in(augmentDir, "settings.json"), events: auggieEvents},
	{ID: Droid, Name: "Droid", Command: "droid", Setup: SetupHooks,
		root: factoryDir, config: in(factoryDir, "hooks.json"), events: droidEvents},
	{ID: Copilot, Name: "Copilot CLI", Command: "copilot", Setup: SetupHooks,
		root: copilotDir, config: in(copilotDir, "hooks", "togo.json"), events: copilotEvents},
	{ID: "vibe", Name: "Mistral Vibe", Command: "vibe", Setup: SetupPending, root: vibeDir},
	{ID: "kimi", Name: "Kimi Code", Command: "kimi", Setup: SetupNone},
	{ID: "opencode", Name: "OpenCode", Command: "opencode", Setup: SetupNone},
	{ID: "kilo", Name: "Kilo Code", Command: "kilo", Setup: SetupNone},
	{ID: "omp", Name: "OMP", Command: "omp", Setup: SetupNone},
	{ID: "pi", Name: "Pi", Command: "pi", Setup: SetupNone},
	{ID: "crush", Name: "Crush", Command: "crush", Setup: SetupNone},
	{ID: "goose", Name: "Goose", Command: "goose", Setup: SetupNone},
	{ID: "aider", Name: "Aider", Command: "aider", Setup: SetupNone},
	{ID: "cline", Name: "Cline", Command: "cline", Setup: SetupNone},
	{ID: "amp", Name: "Amp", Command: "amp", Setup: SetupNone},
	{ID: "cursor", Name: "Cursor CLI", Command: "cursor-agent", Setup: SetupNone},
	{ID: "kiro", Name: "Kiro CLI", Command: "kiro-cli", Setup: SetupNone},
	{ID: "openhands", Name: "OpenHands", Command: "openhands", Setup: SetupNone},
}

// Lookup returns the registry row for id.
func Lookup(id string) (Harness, bool) {
	i := slices.IndexFunc(Registry, func(h Harness) bool { return h.ID == id })
	if i < 0 {
		return Harness{}, false
	}
	return Registry[i], true
}

// Agents lists the harnesses Install and Uninstall accept, in registry
// order.
var Agents = hookAgents()

func hookAgents() []string {
	var out []string
	for _, h := range Registry {
		if h.Setup == SetupHooks {
			out = append(out, h.ID)
		}
	}
	return out
}

// Known reports whether agent is one Install accepts.
func Known(agent string) bool { return slices.Contains(Agents, agent) }

// Present reports whether the harness is installed here: its program is
// found (when Options.HasBinary is set) and, for a harness Togo writes
// config for, its config directory exists.
func (h Harness) Present(o Options) bool {
	if o.HasBinary != nil && h.Command != "" && !o.HasBinary(h.Command) {
		return false
	}
	if h.root == nil {
		return o.HasBinary != nil
	}
	return dirExists(h.Root(o))
}

// Default lists the harnesses a plain `togo hooks install` sets up: every
// one that takes hooks and is installed here.
func Default(o Options) []string {
	var out []string
	for _, h := range Registry {
		if h.Setup == SetupHooks && h.Present(o) {
			out = append(out, h.ID)
		}
	}
	return out
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// DefaultOptions resolves every harness's config root the way the harness
// does: CLAUDE_CONFIG_DIR, CODEX_HOME, GEMINI_CLI_HOME and COPILOT_HOME
// when set, else the dot directory in home.
func DefaultOptions(getenv func(string) string, home, togoHome, binary string) Options {
	pick := func(key, fallback string) string {
		if v := getenv(key); v != "" && filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(home, fallback)
	}
	gemini := filepath.Join(home, ".gemini")
	if v := getenv("GEMINI_CLI_HOME"); v != "" && filepath.IsAbs(v) {
		gemini = filepath.Join(v, ".gemini")
	}
	return Options{
		TogoHome:   togoHome,
		Binary:     binary,
		ClaudeDir:  pick("CLAUDE_CONFIG_DIR", ".claude"),
		CodexDir:   pick("CODEX_HOME", ".codex"),
		GeminiDir:  gemini,
		AugmentDir: filepath.Join(home, ".augment"),
		FactoryDir: filepath.Join(home, ".factory"),
		CopilotDir: pick("COPILOT_HOME", ".copilot"),
		VibeDir:    pick("VIBE_HOME", ".vibe"),
	}
}
