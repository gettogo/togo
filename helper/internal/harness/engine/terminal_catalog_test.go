package engine

import "testing"

// The line a "/" was typed on is the one line that changed by exactly that
// character. It is the proof the screen is still as the reading left it;
// without it no further key is pressed and the "/" is not taken back.
func TestTheLineTheSlashWentOnIsFound(t *testing.T) {
	before := "reply text\n┃  \n┃  Build · model\nstatus"
	after := "reply text\n┃  /\n┃  Build · model\nstatus"
	if i, line, ok := ownLine(before, after); !ok || i != 1 || line != "┃  /" {
		t.Errorf("empty input line: %d %q %v", i, line, ok)
	}
	// A list opening above or below does not confuse it.
	menu := "┃ /agents   Switch agent\n┃ /btw      One-shot answer\nreply text\n┃  /\n┃  Build · model\nstatus"
	padded := "\n\n" + before
	if i, line, ok := ownLine(padded, menu); !ok || i != 3 || line != "┃  /" {
		t.Errorf("with the list drawn: %d %q %v", i, line, ok)
	}
	// A row of the list drawn over a line that held only the frame is not
	// the input line: it does not end with the "/".
	framed := "┃\n┃\n┃  \n┃  Build"
	listed := "┃ /cd      Change working directory\n┃ /clear   Clear session\n┃  /\n┃  Build"
	if i, line, ok := ownLine(framed, listed); !ok || i != 2 || line != "┃  /" {
		t.Errorf("list rows over frame lines: %d %q %v", i, line, ok)
	}
	// An empty field that showed a placeholder.
	if i, line, ok := ownLine("> Ask anything", "> /"); !ok || i != 0 || line != "> /" {
		t.Errorf("placeholder replaced: %d %q %v", i, line, ok)
	}
	// An input line inside a box keeps the box's edge after the "/".
	if i, line, ok := ownLine("│ >                    │", "│ > /                  │"); !ok || i != 0 || line != "│ > /" {
		t.Errorf("boxed input line: %d %q %v", i, line, ok)
	}
	// After a draft the "/" lands at its end.
	if i, line, ok := ownLine("> half written", "> half written/"); !ok || i != 0 || line != "> half written/" {
		t.Errorf("after a draft: %d %q %v", i, line, ok)
	}
	// Nothing changed by a "/": not found.
	if _, _, ok := ownLine("a\nb", "a\nb"); ok {
		t.Error("found a line on an unchanged screen")
	}
	if _, _, ok := ownLine("a\nb", "a\nbx"); ok {
		t.Error("another character was taken for the slash")
	}
	// Two lines that both changed by a "/" (a path printed while typing):
	// not sure which is ours, so none.
	if _, _, ok := ownLine("in\nout", "in/\nout/"); ok {
		t.Error("two candidates were resolved by guessing")
	}
}

// The line a command was typed on is found by what the lines say: the
// harness may have moved it (a list opened above it), and a draft may
// stand after the text.
func TestTheLineACommandWasTypedOnIsFound(t *testing.T) {
	// A scrolling terminal: the list pushed the input line to another row.
	before := "reply\n╰─\nstatus"
	after := "reply\n╰─ /login\n❯ login   Sign in\n  logout  Sign out\nstatus"
	if line, without, ok := ownTextLine(before, after, "/login"); !ok || line != "╰─ /login" || without != "╰─" {
		t.Errorf("moved input line: %q %q %v", line, without, ok)
	}
	// An empty field's placeholder went when the text was typed.
	if line, _, ok := ownTextLine("┃  Ask anything… \"Fix a TODO\"", "┃  /models", "/models"); !ok || line != "┃  /models" {
		t.Errorf("placeholder: %q %v", line, ok)
	}
	// A draft after the cursor, with spaces before it: found, and the line
	// does not end with the text, which is how the draft is known.
	line, without, ok := ownTextLine("┃     spaced draft", "┃  /models   spaced draft", "/models")
	if !ok || line != "┃  /models   spaced draft" || without != "┃     spaced draft" {
		t.Errorf("draft after the cursor: %q %q %v", line, without, ok)
	}
	// A draft before the cursor.
	if line, without, ok := ownTextLine("> half written", "> half written/models", "/models"); !ok || line != "> half written/models" || without != "> half written" {
		t.Errorf("draft before the cursor: %q %q %v", line, without, ok)
	}
	// The text nowhere, or on two lines that both fit: not found.
	if _, _, ok := ownTextLine("a\nb", "a\nb", "/x"); ok {
		t.Error("found on an unchanged screen")
	}
	if _, _, ok := ownTextLine("in\nout", "in/x\nout/x", "/x"); ok {
		t.Error("two candidates were resolved by guessing")
	}
}

// A command the harness shows back counts only when the harness drew it:
// the input line, where a draft starting with the same command stands in
// front of the typed text, is not that.
func TestTheInputLineIsNotTheHarnessShowingACommandBack(t *testing.T) {
	before := "status\n/model neutral\nfooter"
	echo := "status\n/model neutral/model\nfooter"
	if shownBack(before, echo, "/model", "/model neutral/model") {
		t.Error("a draft in the input line passed as the command shown back")
	}
	droid := "│ > /model │\n  /model    Open model selector\n  /fast   Enable fast mode"
	if !shownBack("│ > Try something │", droid, "/model", "> /model") {
		t.Error("a plain list with the command first was not accepted")
	}
	other := "│ > /mod │\n  /mode    Switch mode\n  /model   Open model selector"
	if shownBack("│ > Try something │", other, "/model", "> /model") {
		t.Error("a list with another command first was accepted")
	}
}
