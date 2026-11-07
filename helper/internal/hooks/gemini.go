package hooks

// Gemini is Gemini CLI. Its hooks live in ~/.gemini/settings.json under
// "hooks", in Claude Code's shape ({matcher, hooks: [{type, command}]}), and
// run through the shell. Hooks are on by default (hooksConfig.enabled).
const Gemini = "gemini"

// geminiEvents are the hooks the gemini adapter reads: session id and
// transcript path, and the working flag. Tool events come from the chat
// file, so they are not hooked.
var geminiEvents = []string{"SessionStart", "BeforeAgent", "AfterAgent", "PreCompress", "SessionEnd"}

// geminiEdits edits ~/.gemini/settings.json. Gemini's hooks never answer
// approvals, so --block adds nothing.
func geminiEdits(o Options, add bool) ([]edit, string, error) {
	return claudeShapeEdits(o.Binary, o.GeminiDir, "settings.json", Gemini, geminiEvents, "", false, add)
}
