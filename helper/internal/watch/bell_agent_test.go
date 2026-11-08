package watch

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// A coding agent rings BEL mid-turn and sends OSC 9 "waiting for your
// input" when it idles; with no prompt on screen neither is a need. They
// go out as info (never pushed), so the phone is not told "needs you"
// with nothing waiting. A prompt on screen still reports the prompt.
func TestAgentBellWithoutPromptIsInfo(t *testing.T) {
	h := newHarness(t)
	h.w.entry(sid).agent = "omp"
	h.signal(t, wire.SignalBell, "")
	h.advance(time.Second)
	h.signal(t, wire.SignalOSC9, "Claude is waiting for your input")
	if len(h.events) != 2 {
		t.Fatalf("events = %+v", h.events)
	}
	for _, ev := range h.events {
		if ev.Level != wire.LevelInfo {
			t.Errorf("event %+v pushed as %s", ev, ev.Level)
		}
	}
}
