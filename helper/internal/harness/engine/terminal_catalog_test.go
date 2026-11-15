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
