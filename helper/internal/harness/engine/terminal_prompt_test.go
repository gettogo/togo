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
	// The app requires the screen's actions to be a list, never null.
	if wire, _ := json.Marshal(res.Screen); !strings.Contains(string(wire), `"actions":[]`) {
		t.Errorf("prompt screen on the wire: %s", wire)
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

// A prompt the phone let go of is still on the harness's screen: until it
// is gone, nothing is typed into the session.
func TestAPromptLeftOpenStillHoldsTyping(t *testing.T) {
	raw, err := os.ReadFile("../termmenu/testdata/omp-prompt.ansi")
	if err != nil {
		t.Fatal(err)
	}
	const id = "s_fakefake32"
	e, _ := newFake(t, id, "1.2.3")
	shown := string(raw)
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return shown, nil }
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.readPrompt(ctx, s)
	e.openPrompt(withMenuConv(ctx, s), s, "/connect", p)
	e.setMenu(id, nil) // released
	if _, held := e.menuLeftOpen(s); !held {
		t.Fatal("a released prompt still on the screen does not hold typing")
	}
	if e.heldNatively(s) == nil {
		t.Error("typing is allowed into a prompt left open")
	}
	shown = ""
	if _, held := e.menuLeftOpen(s); held {
		t.Error("the hold outlived the prompt")
	}
}

// The guard a command leaves behind ends with its conversation: it does not
// hold back typing in the next one, even when the input line's look was
// known and is not seen again.
func TestAGuardLeftByACommandEndsWithItsConversation(t *testing.T) {
	const id = "s_fakefake33"
	e, _ := newFake(t, id, "1.2.3")
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	gen := s.convGen
	s.mu.Unlock()
	e.menuMu.Lock()
	e.awaiting = map[string]composerMark{id: {ok: true, prefix: "> ", x: 2, sess: s, conv: gen}}
	e.menuMu.Unlock()
	if !e.awaitingComposer(s) {
		t.Fatal("the guard does not hold in its own conversation")
	}
	s.mu.Lock()
	s.convGen++
	s.mu.Unlock()
	if e.awaitingComposer(s) {
		t.Error("an old command's guard still holds in the next conversation")
	}
}
