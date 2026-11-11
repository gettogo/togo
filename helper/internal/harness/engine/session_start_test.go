package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakestart", []byte(`
adapter: fakestart
name: Fake start
revision: 1
match: { agents: [fakestart] }
capabilities: [chat, send.api, model, status]
feeds:
  events:
    hook: { events: [Event] }
    mapper: fake.events
    stability: documented
actions:
  start_session: { cli: { argv: [rec, start] }, stability: documented }
  show_session: { cli: { argv: [rec, show, '{{.state.harness_session}}'] }, stability: documented }
  set_model: { cli: { argv: [rec, model, '{{.state.harness_session}}', '{{.id}}'] }, stability: documented }
  send: { cli: { argv: [rec, send, '{{.state.harness_session}}', '{{.text}}'] }, stability: documented }
`))
}

// OpenCode and Kilo make their session only with the first prompt, so a
// model picked on the phone before it had nothing to apply to. The pick
// now starts the session (start_session), waits for the feed to name it,
// applies to it, shows it (show_session, last: it may restart the agent), and the first prompt is sent to
// it rather than typed into the agent's own choice.
func TestSettingBeforeFirstPromptStartsTheSession(t *testing.T) {
	const id = "s_fakestart1"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte("#!/bin/sh\necho \"$@\" >> rec.log\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakestart", Managed: true, Cwd: home, Command: []string{"fakestart"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(home, "rec.log")
	// The harness: once started, the feed reports its session.
	go func() {
		for i := 0; i < 200; i++ {
			if b, _ := os.ReadFile(logf); strings.Contains(string(b), "start") {
				data, _ := json.Marshal(map[string]any{"state": map[string]string{"harness_session": "ses_1"}})
				source.Hooks.Deliver(id, source.Payload{Kind: source.BusHook, Event: "Event", Data: data})
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"openrouter/glm"`)}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("set_model before the first prompt = %+v %v", res, err)
	}
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logf)
	want := "start\nmodel ses_1 openrouter/glm\nshow ses_1\nsend ses_1 hello\n"
	if string(b) != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", b, want)
	}
	if _, typed := pane.snapshot(); len(typed) != 0 {
		t.Errorf("typed %q; the first prompt goes to the started session", typed)
	}
	// A second pick does not start another session.
	if res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"x"`)}}); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("second pick %+v %v", res, err)
	}
	if b, _ := os.ReadFile(logf); strings.Count(string(b), "start") != 1 {
		t.Fatalf("started twice:\n%s", b)
	}
}
