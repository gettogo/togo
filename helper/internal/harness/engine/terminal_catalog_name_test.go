package engine

import "testing"

// Seen live: OpenCode 2.2.5's command list, read in a short terminal, gave
// a row that was only the panel's border, and "/┃" was offered as a
// command. A row names a command only by a plain word.
func TestOnlyAPlainWordNamesACommand(t *testing.T) {
	for label, want := range map[string]string{
		"/models": "models", "/native-late  Open a list": "native-late", "source:skill": "source:skill",
		"/mcp__server.tool": "mcp__server.tool", "/q": "q", "/9lives": "9lives",
		"┃": "", "/┃": "", "│ models": "", "(3 more)": "", "/-dash": "", "[x] thing": "", "<name>": "", "": "", "/": "",
		"/模型": "",
	} {
		got, ok := commandName(label)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q, %v; want %q", label, got, ok, want)
		}
	}
}
