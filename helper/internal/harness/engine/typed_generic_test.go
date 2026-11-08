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
