package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakesend", []byte(`
adapter: fakesend
name: Fake send
revision: 1
match: { agents: [fakesend] }
capabilities: [chat, send.api, interrupt]
actions:
  send: { cli: { argv: [rec, send, '{{.state.harness_session}}', '{{.text}}'] }, stability: documented }
  interrupt: { cli: { argv: [rec, abort, '{{.state.harness_session}}'] }, stability: documented }
`))
}

// Seen live on OpenCode 1.18: before the first prompt there is no harness
// session, and POST /session//prompt_async answers 200 and drops the text.
// The first prompt is typed into the TUI instead, and a session-scoped
// action reports that nothing is there yet rather than "applied".
func TestSendBeforeHarnessSessionTypes(t *testing.T) {
	const id = "s_fakesend01"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rec"), []byte("#!/bin/sh\necho \"$@\" >> rec.log\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pane := &seqPane{}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakesend", Managed: true, Cwd: home, Command: []string{"fakesend"}}},
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
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, typed := pane.snapshot(); len(typed) == 0 || typed[0] != "hello" {
		t.Errorf("typed %q, want the prompt typed", typed)
	}
	if _, err := os.Stat(filepath.Join(home, "rec.log")); err == nil {
		t.Error("the send action ran without a harness session")
	}
	res, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "interrupt"})
	if err != nil || res.Result == harness.ResultApplied {
		t.Errorf("interrupt with no session = %+v %v", res, err)
	}
}
