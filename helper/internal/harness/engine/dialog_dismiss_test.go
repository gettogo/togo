package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// splashPane closes its splash on any key but Enter, like Cline 3.0.74's
// "Introducing Cline Desktop" (Enter opens https://cline.bot/desktop).
type splashPane struct{ dialogPane }

func (p *splashPane) Keys(ctx context.Context, id string, keys ...string) error {
	for _, k := range keys {
		if k == "Enter" {
			p.set(clineSplash + "\nBROWSER OPENED")
			return p.seqPane.Keys(ctx, id, keys...)
		}
	}
	p.set("❯ What can I do for you?")
	return p.seqPane.Keys(ctx, id, keys...)
}

const clineSplash = `
   Introducing Cline Desktop
   A native app for working with open weights models.
   https://cline.bot/desktop
   Get Cline Desktop
   Press Enter to open, any other key to close
`

func TestTypingDismissesLinkSplashWithoutEnter(t *testing.T) {
	const id = "s_fakesend03"
	pane := &splashPane{}
	pane.set(clineSplash)
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fakesend", Managed: true, Cwd: t.TempDir(), Command: []string{"fakesend"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: t.TempDir(),
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "hello"}); err != nil {
		t.Fatalf("send after splash = %v", err)
	}
	keys, typed := pane.snapshot()
	if len(keys) == 0 || keys[0] != "Escape" {
		t.Fatalf("keys = %v; want Escape first", keys)
	}
	for _, k := range keys {
		if k == "Enter" {
			t.Fatalf("Enter sent to a link splash: %v", keys)
		}
	}
	if len(typed) != 1 || typed[0] != "hello" {
		t.Fatalf("typed = %q", typed)
	}
}

func TestSafeDismissRefusesEnter(t *testing.T) {
	if safeDismiss([]string{"Enter"}) || safeDismiss(nil) || safeDismiss([]string{"Escape", "Enter"}) || !safeDismiss([]string{"Escape"}) {
		t.Fatal("safeDismiss wrong")
	}
}
