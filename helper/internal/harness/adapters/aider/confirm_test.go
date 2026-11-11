package aider

import (
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// real-confirm-groups.history.md is aider 0.86.2's own history of a run
// that met each kind of confirmation it raises: a plain y/n, a group of
// URLs answered "(S)kip all", shell commands answered y, "(D)on't ask
// again" and n, a group of files answered "(A)ll", and "Create new file?".
// After "(A)ll" or "(S)kip all" aider answers the rest of the group itself
// and logs that answer as a prompt line; it is a step, not the user's.
func TestHistoryGroupAnswersAreNotPrompts(t *testing.T) {
	b, err := os.ReadFile("testdata/real-confirm-groups.history.md")
	if err != nil {
		t.Fatal(err)
	}
	h := newHistory(harness.Env{Cwd: t.TempDir(), Vars: map[string]string{}})
	items, _ := h.Map(record(t, strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")))
	var users []string
	var steps []string
	for _, it := range latest(items) {
		switch it.Kind {
		case harness.KindUser:
			users = append(users, it.User.Text)
		case harness.KindTool:
			steps = append(steps, it.Tool.Title+"\n"+it.Tool.Output)
		}
	}
	want := []string{
		"/run echo hello",
		"/add nosuch.txt",
		"Reply with one word only. Links: https://example.com/a https://example.org/b",
		"Do not edit any files. Suggest two separate shell commands for me to run, each in its own ```bash block: first `echo one`, then `echo two`.",
		"Do not edit anything yet. Reply with only the names of the two files I would change to rename foo, on one line: alpha.py and beta.py",
		"Create a new file gamma.py containing exactly one line: X = 1. Change nothing else.",
	}
	if strings.Join(users, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts:\n%q\nwant:\n%q", users, want)
	}
	all := strings.Join(steps, "\n")
	for _, s := range []string{"[Yes]: skip", "[Yes]: all", "Run shell command? (Y)es/(N)o/(S)kip all/(D)on't ask again [Yes]: n"} {
		if !strings.Contains(all, s) {
			t.Errorf("no step shows %q", s)
		}
	}
}
