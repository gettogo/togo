package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Harnesses whose hooks follow Claude Code's shape (qwen, droid) or Copilot
// CLI's own format. Their hooks are their native chat feed, so setup
// installs them whenever the harness is installed.
const (
	Qwen    = "qwen"
	Droid   = "droid"
	Copilot = "copilot"
)

// qwenEvents are Qwen Code's hooks the qwen adapter reads; they share
// Claude Code's names and shape.
var qwenEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
	"Notification", "Stop", "StopFailure", "SubagentStart", "SubagentStop", "PreCompact", "PostCompact",
	"TodoCreated", "TodoCompleted", "SessionEnd"}

// droidEvents are Droid's hooks (Claude Code's names and shape).
var droidEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification",
	"Stop", "SubagentStop", "PreCompact", "SessionEnd"}

// copilotEvents are Copilot CLI's own hook names.
var copilotEvents = []string{"sessionStart", "userPromptSubmitted", "preToolUse", "postToolUse",
	"postToolUseFailure", "agentStop", "subagentStart", "subagentStop", "errorOccurred", "preCompact",
	"notification", "sessionEnd"}

const copilotPermission = "permissionRequest"

// qwenEdits edits ~/.qwen/settings.json, a Claude-style settings document.
func qwenEdits(o Options, add bool) ([]edit, string, error) {
	return claudeShapeEdits(o.Binary, o.QwenDir, "settings.json", Qwen, qwenEvents, permissionRequest, o.Block, add)
}

// droidEdits edits ~/.factory/hooks.json. Droid never waits for an answer
// from a hook, so --block adds nothing.
func droidEdits(o Options, add bool) ([]edit, string, error) {
	return claudeShapeEdits(o.Binary, o.FactoryDir, "hooks.json", Droid, droidEvents, "", false, add)
}

func claudeShapeEdits(binary, root, file, agent string, events []string, blockEvent string, block, add bool) ([]edit, string, error) {
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	want, all := slices.Clone(events), slices.Clone(events)
	if blockEvent != "" {
		all = append(all, blockEvent)
		if block {
			want = append(want, blockEvent)
		}
	}
	s := spec{agent: agent, binary: binary, add: add, want: want, all: all}
	return []edit{{path: filepath.Join(root, file), transform: func(cur []byte) ([]byte, error) { return editHooksJSON(cur, s) }}}, root, nil
}

// copilotEdits writes ~/.copilot/hooks/togo.json, a file of Togo's own in
// Copilot's hook format ({version: 1, hooks: {event: [{type, bash,
// timeoutSec}]}}). Uninstall deletes it.
func copilotEdits(o Options, add bool) ([]edit, string, error) {
	root := o.CopilotDir
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	return []edit{{path: filepath.Join(root, "hooks", "togo.json"), transform: func(cur []byte) ([]byte, error) {
		if !add {
			return nil, nil
		}
		if err := copilotEdited(cur, o.Binary); err != nil {
			return nil, err
		}
		events := slices.Clone(copilotEvents)
		if o.Block {
			events = append(events, copilotPermission)
		}
		hooks := map[string]any{}
		for _, ev := range events {
			timeout := 30
			if ev == copilotPermission {
				timeout = 600
			}
			hooks[ev] = []any{map[string]any{"type": "command", "bash": hookCommand(o.Binary, Copilot, ev), "timeoutSec": timeout}}
		}
		b, err := json.MarshalIndent(map[string]any{"version": 1, "hooks": hooks}, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}}}, root, nil
}

// copilotEdited refuses to replace a togo.json whose hooks are no longer
// plain Togo commands: the user changed Togo's file, and install leaves it.
func copilotEdited(cur []byte, binary string) error {
	if len(cur) == 0 {
		return nil
	}
	var doc struct {
		Hooks map[string][]struct {
			Bash string `json:"bash"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(cur, &doc); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	for ev, list := range doc.Hooks {
		for _, h := range list {
			if !plainCommand(h.Bash, Copilot) {
				return fmt.Errorf("%w: hooks.%s runs %q", ErrEdited, ev, h.Bash)
			}
		}
	}
	return nil
}
