package local

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestForeign(t *testing.T) {
	cases := []struct {
		owner, agent string
		want         bool
	}{
		{"copilot", "claude", true},
		{"claude", "copilot", true},
		{"qwen", "claude", true},
		{"claude", "claude", false},
		{"copilot", "copilot", false},
		{"shell", "claude", false},
		{"", "claude", false},
		{"not-an-agent", "claude", false},
	}
	for _, c := range cases {
		if got := Foreign(c.owner, c.agent); got != c.want {
			t.Errorf("Foreign(%q, %q) = %v", c.owner, c.agent, got)
		}
	}
}

// Copilot runs Claude's hook file, so a `togo hook claude` call arrives
// for a Copilot session. It must not move the session's status, open an
// approval, push "Claude finished" or reach Copilot's harness feeds; the
// same session's own copilot hooks still do.
func TestClaudeHookInsideCopilotSessionIsDropped(t *testing.T) {
	h, hub, ap, pub := setup()
	lookups := 0
	h.Agents = &AgentCache{Lookup: func(_ context.Context, id string) string {
		lookups++
		if id == sid {
			return "copilot"
		}
		return ""
	}}
	var forwarded []wire.HookForward
	h.Harness = func(_ string, f wire.HookForward) { forwarded = append(forwarded, f) }

	for _, ev := range []string{EventPrompt, EventTool, EventPermission, EventStop, EventHarness} {
		if _, err := call(t, h, wire.MethodLocalHook, hookEv(ev, wire.HookData{Tool: "Bash"})); err != nil {
			t.Fatal(err)
		}
	}
	if hub.status != "" || len(ap.opened) != 0 || len(pub.events) != 0 || len(forwarded) != 0 {
		t.Fatalf("claude hook acted on a copilot session: status %q approvals %d pushes %d forwarded %d",
			hub.status, len(ap.opened), len(pub.events), len(forwarded))
	}

	own := hookEv(EventPermission, wire.HookData{Tool: "bash"})
	own.Agent = "copilot"
	if _, err := call(t, h, wire.MethodLocalHook, own); err != nil {
		t.Fatal(err)
	}
	stop := hookEv(EventStop, wire.HookData{})
	stop.Agent = "copilot"
	if _, err := call(t, h, wire.MethodLocalHook, stop); err != nil {
		t.Fatal(err)
	}
	if len(ap.opened) != 1 || ap.opened[0].Agent != "copilot" || hub.status != statusIdle || len(forwarded) != 2 {
		t.Fatalf("copilot's own hooks: approvals %+v status %q forwarded %d", ap.opened, hub.status, len(forwarded))
	}
	last := pub.events[len(pub.events)-1].payload.(wire.NotifyEvent)
	if last.Title != "Copilot CLI finished" {
		t.Fatalf("done title %q", last.Title)
	}
	if lookups != 1 {
		t.Fatalf("looked the session up %d times, want once", lookups)
	}
}

func TestHookAcceptedWhenSessionAgentUnknown(t *testing.T) {
	h, hub, _, _ := setup()
	h.Agents = &AgentCache{Lookup: func(context.Context, string) string { return "" }}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventPrompt, wire.HookData{})); err != nil {
		t.Fatal(err)
	}
	if hub.status != statusWorking {
		t.Fatalf("status %q", hub.status)
	}
	h.Agents = &AgentCache{Lookup: func(context.Context, string) string { return "shell" }}
	hub.status = ""
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventPrompt, wire.HookData{})); err != nil {
		t.Fatal(err)
	}
	if hub.status != statusWorking {
		t.Fatalf("shell session status %q", hub.status)
	}
}
