package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakeinput reads its input line as omp 18.16 draws it.
func init() {
	harness.RegisterAdapterFile("fakeinput", []byte(`
adapter: fakeinput
name: Fake input
revision: 1
match: { agents: [fakeinput] }
capabilities: [chat, commands]
input:
  line: '^╰─(?: (.*?))?(?:\s{2,}⇧⇥ to change thinking effort)?\s*$'
  clear: [C-u]
  dismiss: [Escape]
actions:
  login: { type: { text: '/login {{.id}}', submit: true }, stability: documented }
`))
}

func screenFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/input/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// inputPane plays an omp input line: idle (empty line), holding text, or
// holding text under an open completion popup.
type inputPane struct {
	mu     sync.Mutex
	state  string // idle, text, popup
	text   string
	keys   []string
	typed  []string
	enters int
	// popupOnType opens the popup when text is typed; enterSubmits says
	// whether Enter with no popup submits; cuClears whether C-u works.
	popupOnType, enterSubmits, cuClears bool
	idle, holding, popup                string
	// completion is what Enter on the open popup puts in the line, as
	// omp 18.16 does; escapeStuck keeps the popup open on Escape.
	completion  string
	escapeStuck bool
	submitted   []string
}

func (p *inputPane) enter() {
	p.enters++
	if p.state == "popup" && p.completion != "" {
		p.text, p.state = p.completion, "text"
		return
	}
	if p.state == "text" && p.enterSubmits {
		p.submitted = append(p.submitted, p.text)
		p.state, p.text = "idle", ""
	}
}

func (p *inputPane) Type(_ context.Context, _, text string, submit, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typed = append(p.typed, text)
	p.text += text
	p.state = "text"
	if p.popupOnType {
		p.state = "popup"
	}
	if submit {
		p.enter()
	}
	return nil
}

func (p *inputPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		p.keys = append(p.keys, k)
		switch k {
		case "Escape":
			if p.state == "popup" && !p.escapeStuck {
				p.state = "text"
			}
		case "Enter":
			p.enter()
		case "C-u":
			if p.cuClears {
				p.state, p.text = "idle", ""
			}
		case "BSpace":
			if r := []rune(p.text); len(r) > 0 {
				p.text = string(r[:len(r)-1])
			}
			if p.text == "" {
				p.state = "idle"
			}
		}
	}
	return nil
}

func (p *inputPane) Screen(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch p.state {
	case "text":
		return strings.Replace(p.holding, "╰─ /login anthropic", "╰─ "+p.text, 1), nil
	case "popup":
		return strings.Replace(p.popup, "╰─ /log", "╰─ "+p.text, 1), nil
	}
	return p.idle, nil
}

func newInputEngine(t *testing.T, id string, p *inputPane) *Engine {
	t.Helper()
	p.idle, p.holding, p.popup = screenFile(t, "omp-idle"), screenFile(t, "omp-leftover"), screenFile(t, "omp-popup")
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeinput", Managed: true, Cwd: t.TempDir(), Command: []string{"fakeinput"}}},
		Typist:   p, Marker: &fakeMarker{}, Screen: p.Screen, Home: t.TempDir(),
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e
}

func login(e *Engine, id string) (harness.ActionResult, error) {
	return e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "login",
		Args: map[string]json.RawMessage{"id": json.RawMessage(`"anthropic"`)}})
}

func TestReadInputOnRecordedScreens(t *testing.T) {
	a, err := compileAdapter(harness.AdapterFiles()["fakeinput"], "test")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"omp-idle": "", "omp-leftover": "/login anthropic", "omp-popup": "/log"} {
		got, ok := a.readInput(screenFile(t, name))
		if !ok || got != want {
			t.Errorf("%s: input = %q, %v; want %q", name, got, ok, want)
		}
	}
}

// Seen live on omp: the completion popup took the Enter of a typed
// /login. The popup is closed with Escape and Enter pressed again.
func TestTypedCommandPopupTookEnter(t *testing.T) {
	const id = "s_fakeinput1"
	p := &inputPane{popupOnType: true, enterSubmits: true}
	e := newInputEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if p.state != "idle" || p.enters != 1 || !strings.Contains(strings.Join(p.keys, " "), "Escape Enter") {
		t.Fatalf("state %s, enters %d, keys %v; want the popup dismissed, then one Enter", p.state, p.enters, p.keys)
	}
	if len(p.submitted) != 1 || p.submitted[0] != "/login anthropic" {
		t.Fatalf("submitted %q", p.submitted)
	}
}

// Seen live on omp 18.16: Enter on the open completion popup applies the
// highlighted entry and keeps it in the line. The popup is closed before
// Enter, so the command typed is the one that runs.
func TestTypedCommandNeverEntersOnPopup(t *testing.T) {
	const id = "s_fakeinput5"
	p := &inputPane{popupOnType: true, enterSubmits: true, completion: "/login amazon-bedrock"}
	e := newInputEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if len(p.submitted) != 1 || p.submitted[0] != "/login anthropic" {
		t.Fatalf("submitted %q; want the typed command, not the completion", p.submitted)
	}
}

// When Enter still lands on the popup and leaves another command in the
// line, the send fails and the line is cleared: never "sent", and never a
// second Enter that would run the completion.
func TestCompletionInLineIsNotCalledSent(t *testing.T) {
	const id = "s_fakeinput6"
	p := &inputPane{popupOnType: true, enterSubmits: true, cuClears: true, escapeStuck: true, completion: "/login amazon-bedrock"}
	e := newInputEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "changed the command") {
		t.Fatalf("login = %+v, %v; want a failure naming the changed command", res, err)
	}
	if len(p.submitted) != 0 || p.text != "" {
		t.Fatalf("submitted %q, line %q; want nothing run and the line cleared", p.submitted, p.text)
	}
}

// A line that never takes Enter is reported, with the text left in place,
// instead of being called sent.
func TestTypedCommandNotSubmittedFails(t *testing.T) {
	const id = "s_fakeinput2"
	p := &inputPane{}
	e := newInputEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "still in its input line") {
		t.Fatalf("login = %+v, %v; want a failure saying the text is still there", res, err)
	}
	if p.text != "/login anthropic" || p.enters != 1+submitRetries {
		t.Fatalf("text %q, enters %d", p.text, p.enters)
	}
}

// Leftover text is cleared before typing; C-u does nothing here, so
// backspaces do it. The owner saw "/login anthropic/model" otherwise.
func TestLeftoverInputIsCleared(t *testing.T) {
	const id = "s_fakeinput3"
	p := &inputPane{enterSubmits: true}
	e := newInputEngine(t, id, p)
	p.state, p.text = "text", "/login anthropic"
	if _, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "/model"}); err != nil {
		t.Fatal(err)
	}
	if len(p.typed) != 1 || p.typed[0] != "/model" || p.state != "idle" {
		t.Fatalf("typed %q, state %s", p.typed, p.state)
	}
	if !strings.Contains(strings.Join(p.keys, " "), "C-u BSpace") {
		t.Fatalf("keys %v; want C-u then backspaces", p.keys)
	}
}

// A send that is not confirmed fails and publishes no prompt.
func TestUnconfirmedSendPublishesNothing(t *testing.T) {
	const id = "s_fakeinput4"
	p := &inputPane{}
	e := newInputEngine(t, id, p)
	_, err := e.Send(context.Background(), harness.SendRequest{Session: id, Text: "hello"})
	var fb *harness.FallbackError
	if !errors.As(err, &fb) {
		t.Fatalf("send = %v; want a fallback", err)
	}
	items, _, _ := e.log.Since(id, 0, 100)
	for _, it := range items {
		if it.User != nil {
			t.Fatalf("published %+v for a prompt that was not submitted", it)
		}
	}
}
