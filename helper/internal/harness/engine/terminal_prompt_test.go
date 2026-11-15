package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/termmenu"
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

// What was entered into a prompt is kept out of everything read from the
// terminal afterwards: the next prompt's title, a list's rows and hints.
// The helper keeps no copy of the entry to do it.
func TestAnEntryIsKeptOutOfWhatTheTerminalShowsNext(t *testing.T) {
	const id = "s_fakefake34"
	e, _ := newFake(t, id, "1.2.3")
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	const entry = "neutral-entry 42"
	e.noteEntry(id, "  "+entry+" ")
	if kept := fmt.Sprintf("%+v", *e.entered[id]); strings.Contains(kept, "neutral") {
		t.Fatal("the entry itself is kept")
	}
	m := &terminalMenu{sess: s, token: "tok", panel: "x", prompt: true}
	sc := e.promptScreen(m, termmenu.Prompt{Title: "Invalid: " + entry + ". Try again"})
	list := &terminalMenu{sess: s, token: "tok2", rows: []termmenu.Item{
		{Label: "Use " + entry, Detail: "saved as " + entry, Group: entry}, {Label: "Other"}}}
	lsc := e.menuScreen(list, termmenu.Menu{Title: "Keys (" + entry + ")"}, true)
	for name, screen := range map[string]*harness.Screen{"prompt": sc, "list": lsc} {
		wire, _ := json.Marshal(screen)
		if strings.Contains(string(wire), entry) || strings.Contains(string(wire), "neutral-entry") {
			t.Errorf("%s screen repeats the entry: %s", name, wire)
		}
	}
	if sc.Title != "Invalid: "+redacted+". Try again" || lsc.Picker.Items[1].Title != "Other" {
		t.Errorf("redaction changed more than the entry: %q / %q", sc.Title, lsc.Picker.Items[1].Title)
	}
	if got := e.redact("s_other", entry); got != entry {
		t.Error("another session's text was redacted")
	}
}

// An entry stays recognised for as long as the session is known: there is no
// time after which, and no number of later entries after which, the harness
// repeating it would show it. When no more can be remembered, a further
// entry is refused rather than an older one forgotten.
func TestAnEntryIsNeverForgottenToMakeRoom(t *testing.T) {
	const id = "s_fakefake35"
	e, _ := newFake(t, id, "1.2.3")
	if !e.noteEntry(id, "first-entry") {
		t.Fatal("the first entry was refused")
	}
	n := 1
	for ; n < enteredKept+10; n++ {
		if !e.noteEntry(id, "later-"+strings.Repeat("x", n)) {
			break
		}
	}
	if n != enteredKept {
		t.Fatalf("%d entries were taken, want %d and then refusal", n, enteredKept)
	}
	if got := e.redact(id, "Invalid first-entry"); strings.Contains(got, "first-entry") {
		t.Errorf("the first entry shows again after %d more: %q", n-1, got)
	}
	if got := e.redact(id, "later-"+strings.Repeat("x", n-1)); !strings.Contains(got, redacted) {
		t.Errorf("the last entry taken is not recognised: %q", got)
	}
}

// The digests live in memory. After the helper restarts, a prompt the
// harness still shows may repeat an entry this helper cannot recognise. A
// marker on disk, which holds nothing about any entry, says so: nothing
// read from that session's terminal is passed on, until the session is gone.
func TestAfterARestartNothingIsShownFromATerminalThatTookEntries(t *testing.T) {
	const id = "s_fakefake36"
	e, _ := newFake(t, id, "1.2.3")
	e.cfg.TogoHome = t.TempDir()
	e.cfg.StyledScreen = func(context.Context, string) (string, error) { return "", nil }
	const entry = "neutral-entry 43"
	if !e.noteEntry(id, entry) {
		t.Fatal("the entry was refused")
	}
	marker := e.enteredMarker(id)
	if info, err := os.Stat(marker); err != nil || info.Size() != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("marker = %v %v", info, err)
	}
	if e.terminalBarred(id) {
		t.Fatal("barred while the entries are still recognised")
	}
	// The restart: what was in memory is gone, the marker is not.
	e.menuMu.Lock()
	e.entered = nil
	e.menuMu.Unlock()
	if !e.terminalBarred(id) {
		t.Fatal("not barred after the digests were lost")
	}
	if got := e.redact(id, "Invalid "+entry); strings.Contains(got, "neutral") {
		t.Errorf("text read after the restart still shows the entry: %q", got)
	}
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "terminal.attach"})
	if err != nil || res.Result != harness.ResultFallback || res.Screen != nil {
		t.Errorf("attach after the restart = %+v %v", res, err)
	}
	if e.noteEntry("../escape", "x") && e.enteredMarker("../escape") != "" {
		t.Error("a session name was used as a path")
	}
	e.Forget(id)
	if _, err := os.Stat(marker); err == nil || e.terminalBarred(id) {
		t.Error("the marker outlived its session")
	}
}
