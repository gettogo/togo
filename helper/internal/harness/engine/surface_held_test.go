package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A phone holds the session's terminal natively: the agent would take keys
// in the pane for someone at the desk and end the native screen. A prompt
// sent now and a typed action are refused with the reason, a queued prompt
// waits and goes out after the phone lets go, and the session says it can
// show native screens only while it can.
func TestNothingIsTypedWhileAPhoneHoldsTheTerminal(t *testing.T) {
	const id = "s_fakefake02"
	e, pane := newFake(t, id, "1.2.3")
	var held, available atomic.Bool
	held.Store(true)
	e.cfg.SurfaceHeld = func(string) bool { return held.Load() }
	e.cfg.SurfaceAvailable = func(context.Context, string) bool { return available.Load() }
	ctx := context.Background()
	d, err := e.Describe(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if harness.Has(d.Capabilities, harness.CapSurface) {
		t.Error("the session says it can show native screens while it cannot")
	}
	available.Store(true)
	if d, _ = e.Describe(ctx, id); !harness.Has(d.Capabilities, harness.CapSurface) {
		t.Error("the session does not say it can show native screens")
	}
	typed := func() string {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		return strings.Join(pane.typed, "|")
	}
	var fb *harness.FallbackError
	if _, err := e.Send(ctx, harness.SendRequest{Session: id, Text: "right now", Mode: harness.SendNow}); !errors.As(err, &fb) || !strings.Contains(fb.Detail, "Nothing was typed") {
		t.Fatalf("send now while held = %v, want a refusal that says nothing was typed", err)
	}
	if r, err := e.Act(ctx, harness.ActionRequest{Session: id, Action: "set_model", Args: map[string]json.RawMessage{"id": json.RawMessage(`"x"`)}}); err == nil && r.Result != harness.ResultFailed && r.Result != harness.ResultFallback {
		t.Errorf("typed action while held = %+v, want it refused", r)
	}
	queue(t, e, id, "later")
	if got := typed(); got != "" {
		t.Fatalf("typed %q while the phone holds the terminal", got)
	}
	held.Store(false)
	waitFor(t, func() bool { return strings.Contains(typed(), "later") })
	if strings.Contains(typed(), "right now") {
		t.Errorf("the refused prompt was typed after all: %q", typed())
	}
}
