package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// dialogPane is a pane whose screen the test sets; it records typing.
type dialogPane struct {
	seqPane
	smu    sync.Mutex
	screen string
}

func (p *dialogPane) Screen(context.Context, string) (string, error) {
	p.smu.Lock()
	defer p.smu.Unlock()
	return p.screen, nil
}

func (p *dialogPane) set(s string) {
	p.smu.Lock()
	p.screen = s
	p.smu.Unlock()
}

const updateDialog = `
                ┃  Update Available                                  esc ┃
                ┃  A new release v1.19.0 is available. Update now?       ┃
                ┃                                    Cancel   Confirm    ┃
`

// Seen live: a prompt typed into OpenCode while its update dialog was up
// confirmed the update. Nothing is typed while a dialog shows; the chat
// says why, and the phone is told once.
func TestTypingWaitsOutDialogs(t *testing.T) {
	const id = "s_fakesend02"
	pane := &dialogPane{}
	pane.set(updateDialog)
	var mu sync.Mutex
	var needs []string
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fakesend", Managed: true, Cwd: t.TempDir(), Command: []string{"fakesend"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: t.TempDir(),
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		NeedsYou: func(id, title, body string) {
			mu.Lock()
			needs = append(needs, title+"|"+body)
			mu.Unlock()
		},
		Log:   &Log{NewEpoch: func() string { return "e1" }},
		Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "hello"})
		var fb *harness.FallbackError
		if !errors.As(err, &fb) || !strings.Contains(fb.Detail, "Update Available") {
			t.Fatalf("send with a dialog up = %v; want a fallback naming the dialog", err)
		}
	}
	if _, typed := pane.snapshot(); len(typed) != 0 {
		t.Fatalf("typed %q into a dialog", typed)
	}
	mu.Lock()
	if len(needs) != 1 || !strings.Contains(needs[0], "Update Available") {
		t.Fatalf("needs-you = %q; want one naming the dialog", needs)
	}
	mu.Unlock()
	items, _, _ := e.log.Since(id, 0, 100)
	found := false
	for _, it := range items {
		if it.Notice != nil && strings.Contains(it.Notice.Text, "Nothing was typed") {
			found = true
		}
	}
	if !found {
		t.Error("no chat notice for the dialog")
	}
	pane.set("  ┃  Ask anything…\n")
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, typed := pane.snapshot(); len(typed) != 1 || typed[0] != "hello" {
		t.Fatalf("typed %q after the dialog closed", typed)
	}
}
