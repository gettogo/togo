package claude

import (
	"path/filepath"
	"strings"
)

// conversationOf names the conversation a main transcript file holds: its
// base name, the session id Claude Code names the file by. /clear and
// /resume move the transcript feed to another file, and the engine starts
// the chat over on it; the same file read again (a helper restart) names
// the same one. A subagent's file, or a record with no file, names none.
func conversationOf(file string) string {
	if file == "" || filepath.Base(filepath.Dir(file)) == "subagents" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(file), ".jsonl")
}
