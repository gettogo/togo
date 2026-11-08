package hooks

import "errors"

// Gemini is Gemini CLI, which Togo no longer runs. Earlier versions wrote
// hooks into ~/.gemini/settings.json in Claude Code's shape; Uninstall still
// removes them (and restores the backup when nothing else changed), so an
// old install can be cleaned up. Install refuses it.
const Gemini = "gemini"

// Retired lists harnesses Togo no longer sets up but whose old hook entries
// Uninstall still removes.
var Retired = []string{Gemini}

// geminiEvents are the hooks earlier versions installed.
var geminiEvents = []string{"SessionStart", "BeforeAgent", "AfterAgent", "PreCompress", "SessionEnd"}

// errRetired refuses an install for a retired harness.
var errRetired = errors.New("no longer supported; uninstall removes its old hooks")

// geminiEdits removes Togo's old entries from ~/.gemini/settings.json.
func geminiEdits(o Options, add bool) ([]edit, string, error) {
	if add {
		return nil, "", errRetired
	}
	return claudeShapeEdits(o.Binary, o.GeminiDir, "settings.json", Gemini, geminiEvents, "", false, false)
}
