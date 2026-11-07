package auggie

import (
	"encoding/json"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Logged out, `auggie model list --json` prints a sentence: the picker is
// empty, not unreadable. JSON output lists the models.
func TestModelsReadOnlyJSON(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	sel := a.Sources["models"].Select
	for text, want := range map[string]int{
		"Not logged in. Run auggie login.": 0,
		`[{"id":"sonnet"},{"id":"gpt"}]`:  2,
		`{"models":[{"id":"sonnet"}]}`:    1,
	} {
		raw, _ := json.Marshal(text)
		var in any
		json.Unmarshal(raw, &in)
		out, err := engine.JQ(sel, in, nil)
		if err != nil || len(out) != 1 || len(out[0].([]any)) != want {
			t.Errorf("%q: %v %v, want %d", text, out, err, want)
		}
	}
}

// Model and effort relaunch into this conversation, never the newest one;
// before the hooks name it there is no route.
func TestRelaunchResumesThisConversation(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"set_model", "set_effort"} {
		br := a.Actions[name].Branch
		if len(br) != 1 || br[0].When == "" || br[0].Relaunch == nil {
			t.Fatalf("%s %+v", name, br)
		}
		argv := br[0].Relaunch.Argv
		if argv[len(argv)-2] != "--resume" || argv[len(argv)-1] != "{{.state.harness_session}}" {
			t.Errorf("%s argv %v", name, argv)
		}
	}
	got, _ := engine.Render(a.Screens["resume"].Map["item.title"], map[string]any{"firstUserMessage": "fix the tests", "sessionId": "x"})
	if got != "fix the tests" {
		t.Errorf("session title %q", got)
	}
}
