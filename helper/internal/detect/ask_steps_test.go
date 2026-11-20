package detect

import (
	"os"
	"regexp"
	"testing"
)

// plainScreen is a recorded pane (escape sequences and all) as tmux's
// capture-pane gives it: the text only.
var escapes = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]|\x1b\\][^\x1b\a]*(\x1b\\\\|\a)")

func plainScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../harness/adapters/claude/testdata/screens/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return escapes.ReplaceAllString(string(b), "")
}

// Every step of Claude Code's two-question dialog (2.1.329, recorded):
// each question and the review are marked as the question dialog, so the
// question item that stands for the dialog is not shadowed by a request
// per step. The screen after it is not.
func TestDetectMarksEveryStepOfTheQuestionDialog(t *testing.T) {
	for i, want := range []struct {
		title string
		ask   bool
	}{{"Which colour do you prefer?", true}, {"Which fruit do you prefer?", true}, {"", true}} {
		p, ok := Detect(plainScreen(t, "ask-step"+string(rune('1'+i))+".ansi"))
		if !ok || p.Ask != want.ask || want.title != "" && p.Title != want.title {
			t.Errorf("step %d: %v %+v", i+1, ok, p)
		}
	}
	if p, ok := Detect(plainScreen(t, "ask-step4.ansi")); ok && p.Ask {
		t.Errorf("after the dialog: %+v", p)
	}
}

// Permission menus are not the question dialog.
func TestPermissionMenuIsNotTheQuestionDialog(t *testing.T) {
	for _, name := range []string{"bash-permission", "edit-permission", "plan-approval"} {
		b, err := os.ReadFile("testdata/claude/" + name + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		if p, ok := Detect(string(b)); !ok || p.Ask {
			t.Errorf("%s: %v %+v", name, ok, p.Ask)
		}
	}
}
