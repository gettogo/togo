package detect

import (
	"os"
	"strings"
	"testing"
)

// ParseChoice on the real Vibe, Copilot, Cline and Droid dialogs: what the
// screen_answer driver needs to match the item's prompt and labels and to
// pick the right keys.
func TestHarnessChoices(t *testing.T) {
	cases := []struct {
		file    string
		prompt  string
		labels  string // the rows' labels, "|"-joined, the text row last
		cursor  int
		textRow int
		digits  bool
		multi   bool
		typing  bool
		tabs    int
	}{
		{"vibe/real-ask-single", "Which database should we use?", "Postgres|SQLite|MongoDB|Type your answer...", -1, 3, true, false, false, 0},
		{"vibe/real-ask-batch-q1", "Which areas should change?", "Backend|Frontend|Docs|Type your answer...", -1, 3, true, true, false, 2},
		{"vibe/real-ask-batch-q2-other-typed", "How should we roll out?", "Canary|Big bang|staged by region", -1, 2, true, false, true, 2},
		{"vibe/real-permission-80", "Permission for the file_system.bash tool (touch *)", "Allow once|Allow for remainder of this session|Always allow|Deny", -1, -1, true, false, false, 0},
		{"copilot/real-ask-single-moved", "Which color should I use for the README title?", "Red|Green|Blue|Other (type your answer)", 1, 3, false, false, false, 0},
		{"copilot/real-ask-batch-multi", "Platforms", "iOS|Android|Web|Other (type your answer)", 0, 3, false, true, false, 3},
		{"copilot/real-ask-other-selected", "Which log level should the app use?", "Debug|Info|Other (type your answer below)", 2, 2, false, false, true, 0},
		{"copilot/real-plan-review", "Plan Ready for Review", "Accept plan and continue in Autopilot execution mode|Accept plan and continue in Interactive execution mode|Exit to Interactive execution mode without running the plan|Suggest changes", 0, -1, true, false, false, 0},
		{"cline/real-ask-single-moved", "Which language would you like me to use for the hello world script?", "Go|Python|Rust|Type a response...", 1, 3, false, false, false, 0},
		{"cline/real-ask-text", "What should the greeting text say?", "Hello|Hi there|Howdy partner", 2, 2, false, false, true, 0},
		{"droid/real-ask-single-80x24", "Which color?", "Red|Green|Blue|Or type your own answer...", -1, 3, true, false, false, 0},
		{"droid/real-spec-approval-80x24", "Propose Specification \"Create `hello.txt` in the working directory.\"", "Proceed with the proposal|Proceed with comment|Proceed and handoff to a new session|Manually edit spec (open via system default)|No and explain why", -1, -1, true, false, false, 0},
		{"droid/real-spec-approval-47", "Propose Specification", "Proceed with the proposal|Proceed with comment|Proceed and handoff to a new session|Manually edit spec (open via system default)|No and explain why", -1, -1, true, false, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + tc.file + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			c, ok := ParseChoice(string(b))
			if !ok {
				t.Fatal("no choice")
			}
			var labels []string
			for _, o := range c.Options {
				labels = append(labels, o.Label)
			}
			if c.Prompt != tc.prompt || strings.Join(labels, "|") != tc.labels {
				t.Fatalf("prompt %q labels %q", c.Prompt, strings.Join(labels, "|"))
			}
			if c.Cursor != tc.cursor || c.TextRow != tc.textRow || c.Digits != tc.digits || c.Multi != tc.multi || c.Typing != tc.typing || c.Tabs != tc.tabs {
				t.Fatalf("cursor %d text %d digits %v multi %v typing %v tabs %d", c.Cursor, c.TextRow, c.Digits, c.Multi, c.Typing, c.Tabs)
			}
		})
	}
}

// Vibe's descriptions and Copilot's wrapped plan parentheses become the
// rows' details, not part of their labels.
func TestHarnessChoiceDetails(t *testing.T) {
	b, _ := os.ReadFile("testdata/vibe/real-ask-single.txt")
	c, _ := ParseChoice(string(b))
	if c.Options[1].Detail != "Embedded file" {
		t.Fatalf("vibe detail %q", c.Options[1].Detail)
	}
	b, _ = os.ReadFile("testdata/copilot/real-plan-review.txt")
	c, _ = ParseChoice(string(b))
	if c.Options[1].Detail != "Manual Approval for this session; future defaults unchanged" {
		t.Fatalf("copilot detail %q", c.Options[1].Detail)
	}
}
