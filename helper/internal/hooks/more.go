package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// Agents Install accepts only when named: their hooks feed Togo's native
// views but writing them changes another harness's config, so "all" and
// Status's default list leave them out.
const (
	Qwen    = "qwen"
	Droid   = "droid"
	Copilot = "copilot"
)

// Optional lists the agents installed only on request.
var Optional = []string{Qwen, Droid, Copilot}

// Known reports whether agent is one Install accepts.
func Known(agent string) bool {
	return slices.Contains(Agents, agent) || slices.Contains(Optional, agent)
}

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
	return []edit{{path: filepath.Join(root, "hooks", "togo.json"), transform: func([]byte) ([]byte, error) {
		if !add {
			return nil, nil
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

// optionalStatus is status for the Optional agents.
func optionalStatus(o Options, agent string) string {
	var root, path string
	var events []string
	switch agent {
	case Qwen:
		root, path, events = o.QwenDir, filepath.Join(o.QwenDir, "settings.json"), qwenEvents
	case Droid:
		root, path, events = o.FactoryDir, filepath.Join(o.FactoryDir, "hooks.json"), droidEvents
	case Copilot:
		root, path, events = o.CopilotDir, filepath.Join(o.CopilotDir, "hooks", "togo.json"), copilotEvents
	}
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return StatusNoAgent
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StatusAbsent
	}
	have, err := ownedEvents(data, agent)
	if err != nil {
		return StatusAbsent
	}
	return coverage(have, events)
}
