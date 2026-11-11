package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakenoinput types commands like Pi, Copilot, Goose, Kimi, Droid,
// Auggie, Vibe and Aider: no input line pattern declared.
func init() {
	harness.RegisterAdapterFile("fakenoinput", []byte(`
adapter: fakenoinput
name: Fake TUI
revision: 1
match: { agents: [fakenoinput] }
capabilities: [chat, commands]
actions:
  login: { type: { text: '/login {{.id}}', submit: true }, stability: documented }
`))
}

func newGenericEngine(t *testing.T, id string, p *inputPane) *Engine {
	t.Helper()
	p.idle, p.holding, p.popup = screenFile(t, "omp-idle"), screenFile(t, "omp-leftover"), screenFile(t, "omp-popup")
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakenoinput", Managed: true, Cwd: t.TempDir(), Command: []string{"fakenoinput"}}},
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

// With no input pattern, the row the typed command shows on is read back
// after Enter: still holding it means the TUI did not take the Enter.
func TestGenericTypedCommandNotTakenFails(t *testing.T) {
	const id = "s_fakegen001"
	p := &inputPane{}
	e := newGenericEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "still") {
		t.Fatalf("login = %+v, %v; want a failure", res, err)
	}
	if p.enters != 2 {
		t.Fatalf("enters %d, want one retry", p.enters)
	}
}

// A completion popup that took the Enter and put another command in the
// row is never called sent, and the row is emptied.
func TestGenericCompletionIsNotCalledSent(t *testing.T) {
	const id = "s_fakegen002"
	p := &inputPane{popupOnType: true, enterSubmits: true, completion: "/login amazon-bedrock"}
	e := newGenericEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result != harness.ResultFailed || !strings.Contains(res.Message, "changed the command") {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if len(p.submitted) != 0 || p.text != "" {
		t.Fatalf("submitted %q, line %q", p.submitted, p.text)
	}
}

// A command the TUI takes is sent once.
func TestGenericTypedCommandTaken(t *testing.T) {
	const id = "s_fakegen003"
	p := &inputPane{enterSubmits: true}
	e := newGenericEngine(t, id, p)
	res, err := login(e, id)
	if err != nil || res.Result == harness.ResultFailed {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if p.enters != 1 || len(p.submitted) != 1 || p.submitted[0] != "/login anthropic" {
		t.Fatalf("enters %d submitted %q", p.enters, p.submitted)
	}
}

// A footer's "/ commands" key hint is no command: Copilot CLI 1.0.107 drew
// "· / commands   Claude Haiku 5.5" under a /model it had taken, and the
// switch was reported as changed to another command.
func TestRowCommandSkipsKeyHints(t *testing.T) {
	for row, want := range map[string]string{
		" ← open sidebar · Interactive · Manual Approval · / commands   Claude Haiku 5.5": "",
		"❯ /model claude-haiku-4.5": "/model claude-haiku-4.5",
		"│ /login amazon-bedrock │": "/login amazon-bedrock",
		"/":                         "",
	} {
		if got := rowCommand(row); got != want {
			t.Errorf("rowCommand(%q) = %q, want %q", row, got, want)
		}
	}
}

// Goose keeps a sent line on screen as "> /mode chat" and draws a fresh
// "> " input line under its reply: the command was taken, and no second
// Enter goes in. A line still at the bottom with nothing under it is not.
func TestNewPromptBelowTheEcho(t *testing.T) {
	taken := []string{"> Reply with only the word ok.", "ok", "> /mode chat", "", "mode: Goose mode set to 'chat'",
		"  ━━━━╌╌╌╌ 21% 14k/66k", "> Enter to send · Ctrl+J newline"}
	if !newPromptBelow(taken, 2, "/mode chat") {
		t.Fatal("goose's fresh input line was not seen")
	}
	waiting := []string{"> Reply with only the word ok.", "ok", "> /mode chat", "  /mode   Set the goose mode"}
	if newPromptBelow(waiting, 2, "/mode chat") {
		t.Fatal("a popup row counted as a new input line")
	}
	if newPromptBelow([]string{"/mode chat", "/mode chat x"}, 0, "/mode chat") {
		t.Fatal("a row with no prompt mark")
	}
}
