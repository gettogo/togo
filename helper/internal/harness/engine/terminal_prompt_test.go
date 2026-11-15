package engine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A prompt is answered only through its own field and interaction, only
// with one line of text, and what is entered goes in by the paste path:
// never as keys, which travel as process arguments.
func TestAPromptTakesOneLineThroughItsOwnField(t *testing.T) {
	raw, err := os.ReadFile("../termmenu/testdata/omp-prompt.ansi")
	if err != nil {
		t.Fatal(err)
	}
	const id = "s_fakefake31"
	e, pane := newFake(t, id, "1.2.3")
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return string(raw), nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := e.readPrompt(ctx, s)
	if !ok {
		t.Fatal("the prompt is not read")
	}
	res := e.openPrompt(withMenuConv(ctx, s), s, "/connect", p)
	field := res.Screen.Settings.Sections[0].Fields[0]
	if res.Screen.Primitive != harness.PrimSettings || field.ID != promptField || field.Kind != harness.FieldSecret ||
		string(field.Value) != "null" || field.IsSet || field.Action != promptAction || field.ActionLabel != "Continue" ||
		field.Presentation != "prompt" || !strings.HasPrefix(res.Screen.ID, "terminal.prompt/") || res.Screen.Interaction == "" {
		t.Fatalf("prompt screen = %+v", res.Screen)
	}
	token, _ := json.Marshal(res.Screen.Interaction)
	typed := func() (texts, keys int) {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return len(pane.typed), len(pane.keys)
	}
	texts, keys := typed()
	act := func(target, value, interaction string) harness.ActionResult {
		v, _ := json.Marshal(value)
		res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "terminal.input", Target: target,
			Args: map[string]json.RawMessage{"value": v, "interaction": json.RawMessage(interaction)}})
		if err != nil {
			return harness.ActionResult{Result: harness.ResultFailed, Message: err.Error()}
		}
		return res
	}
	for name, r := range map[string]harness.ActionResult{
		"another field":   act("other.field", "x", string(token)),
		"another token":   act(promptField, "x", `"nope"`),
		"an empty entry":  act(promptField, "", string(token)),
		"a second line":   act(promptField, "a\nb", string(token)),
		"an escape":       act(promptField, "a\x1b[2J", string(token)),
		"too long a line": act(promptField, strings.Repeat("x", maxPromptLen+1), string(token)),
	} {
		if r.Result != harness.ResultFailed {
			t.Errorf("%s was accepted: %+v", name, r)
		}
		if strings.Contains(r.Message, "a\nb") {
			t.Errorf("%s: the refusal repeats the entry", name)
		}
	}
	if a, b := typed(); a != texts || b != keys {
		t.Fatalf("something was typed for a refused entry")
	}
	// The screen never shows the field taking text (it is a fixed capture),
	// so the entry is pasted, not seen, and Enter is not pressed.
	r := act(promptField, "neutral entry", string(token))
	a, b := typed()
	if a != texts+1 || b != keys {
		t.Errorf("entry went as %d pastes and %d keys, want one paste and no key: %+v", a-texts, b-keys, r)
	}
	if r.Result == harness.ResultApplied || strings.Contains(r.Message, "neutral entry") {
		t.Errorf("an entry never seen in the field was reported done, or repeated: %+v", r)
	}
}
