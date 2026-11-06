package local

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestEndHookEndsSession(t *testing.T) {
	h, hub, _, pub := setup()
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventEnd, wire.HookData{})); err != nil {
		t.Fatal(err)
	}
	if hub.exitStatus != wire.StatusEnded || hub.exit == nil || hub.exit.Reason != wire.ExitReasonExit || hub.exit.Code == nil || *hub.exit.Code != 0 {
		t.Fatalf("exit %q %+v, want ended/exit 0", hub.exitStatus, hub.exit)
	}
	if len(pub.events) != 0 {
		t.Fatalf("an end published %+v; it must never be a done", pub.events)
	}
}

func TestFailureHooksFailSession(t *testing.T) {
	for _, c := range []struct{ event, reason string }{
		{EventFailure, wire.ExitReasonError},
		{EventUsageLimit, wire.ExitReasonUsageLimit},
	} {
		h, hub, _, pub := setup()
		msg := "\x1b[31mAPI Error:\x1b[0m ╭── Rate​ limit ──╮\nsecond line " + strings.Repeat("x", 300)
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(c.event, wire.HookData{Message: msg})); err != nil {
			t.Fatal(err)
		}
		if hub.exitStatus != wire.StatusFailed || hub.exit == nil || hub.exit.Reason != c.reason || hub.exit.Code != nil {
			t.Fatalf("%s: exit %q %+v", c.event, hub.exitStatus, hub.exit)
		}
		if err := hub.exit.Validate(); err != nil || hub.exit.Detail == nil || *hub.exit.Detail != "API Error: Rate limit" {
			t.Fatalf("%s: detail %q (%v)", c.event, *hub.exit.Detail, err)
		}
		if len(pub.events) != 0 {
			t.Fatalf("%s published %+v", c.event, pub.events)
		}
	}
}

// TestHooksThatClearExit: an agent working again clears the exit; its
// status line, notifications and its own exit reports do not.
func TestHooksThatClearExit(t *testing.T) {
	for _, ev := range []string{EventPrompt, EventTool, EventToolDone, EventPermission, EventStop, EventStart} {
		h, hub, _, _ := setup()
		hub.exitStatus = wire.StatusFailed
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{})); err != nil {
			t.Fatal(err)
		}
		if hub.clears != 1 {
			t.Errorf("%s: %d clears, want 1", ev, hub.clears)
		}
	}
	for _, ev := range []string{EventMeta, EventNotify, EventAttention, EventEnd, EventFailure, EventUsageLimit} {
		h, hub, _, _ := setup()
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{})); err != nil {
			t.Fatal(err)
		}
		if hub.clears != 0 {
			t.Errorf("%s cleared the exit", ev)
		}
	}
}

func TestExitDetail(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"  \x1b[2J  ":            "",
		"│ ok │":                 "ok",
		"one\ntwo":               "one",
		strings.Repeat("é", 250): strings.Repeat("é", 200),
	} {
		got := ExitDetail(in)
		if (got == nil) != (want == "") || (got != nil && *got != want) {
			t.Errorf("%.20q: got %v, want %q", in, got, want)
		}
	}
}

// TestExitHookFromKilledRunIgnored: while the hub waits for the new run,
// an end or failure hook changes nothing; the new run's first hook lets
// them through again.
func TestExitHookFromKilledRunIgnored(t *testing.T) {
	for _, ev := range []string{EventEnd, EventFailure, EventUsageLimit} {
		h, hub, ap, _ := setup()
		hub.stale = true
		ap.pending = []wire.Approval{{RequestID: "r_bbbbbbbbbb", Source: wire.SourceHook}}
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{})); err != nil {
			t.Fatal(err)
		}
		if hub.exitStatus != "" || len(ap.closed) != 0 {
			t.Fatalf("%s from the killed run: exit %q, closed %v", ev, hub.exitStatus, ap.closed)
		}
	}
}
