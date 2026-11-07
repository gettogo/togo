package hooks

import (
	"os"
	"path/filepath"
)

// codexEvents are the events the Codex adapter maps: session start,
// prompt, tool start and end, permission and stop. The tool events feed the
// native chat while the app-server is not connected.
var codexEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Stop"}

// codexEdits edits hooks.json and config.toml, where the [features] hooks
// flag turns on loading of hooks.json.
func codexEdits(o Options, add bool) ([]edit, string, error) {
	if st, err := os.Stat(o.CodexDir); o.CodexDir == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	s := spec{agent: Codex, binary: o.Binary, want: codexEvents, all: codexEvents, add: add}
	return []edit{
		{path: filepath.Join(o.CodexDir, "hooks.json"), transform: func(cur []byte) ([]byte, error) { return editHooksJSON(cur, s) }},
		{path: filepath.Join(o.CodexDir, "config.toml"), transform: func(cur []byte) ([]byte, error) {
			if add {
				return enableHooksFlag(cur)
			}
			return disableHooksFlag(cur)
		}},
	}, o.CodexDir, nil
}
