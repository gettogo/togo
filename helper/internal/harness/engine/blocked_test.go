package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The owner's Aider at 47 columns: a chat send landed in "Login to
// OpenRouter or create a free account? (Y)es/(N)o [Yes]:". With the
// question on screen a send that must go now is refused with the reason,
// a queued send waits, and nothing is typed until the question is gone.
func TestSendNeverTypesIntoAQuestion(t *testing.T) {
	const id = "s_fakeblock1"
	b, err := os.ReadFile("../../detect/testdata/aider/real-openrouter-reask-47col.txt")
	if err != nil {
		t.Fatal(err)
	}
	pane := &dialogPane{}
	pane.set(string(b))
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

	_, err = e.Send(ctx, harness.SendRequest{Session: id, Text: "testing to see if togo is working?", Mode: harness.SendNow})
	var fb *harness.FallbackError
	if !errors.As(err, &fb) || !strings.Contains(fb.Detail, "Login to OpenRouter") || !strings.Contains(fb.Detail, "Nothing was typed") {
		t.Fatalf("send now into a question = %v; want a refusal naming it", err)
	}

	res, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "queued while asked", Mode: harness.SendQueue})
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.User == nil || !res.Item.User.Queued {
		t.Fatalf("send during a question = %+v; want it queued", res.Item)
	}
	time.Sleep(500 * time.Millisecond)
	if _, typed := pane.snapshot(); len(typed) != 0 {
		t.Fatalf("typed %q into the question", typed)
	}

	pane.set("> \n")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, typed := pane.snapshot(); len(typed) == 1 {
			if typed[0] != "queued while asked" {
				t.Fatalf("typed %q", typed)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, typed := pane.snapshot()
	t.Fatalf("queued prompt not typed after the question closed: %q", typed)
}
