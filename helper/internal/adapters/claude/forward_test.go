package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestForwardRedactsAndBounds(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	in := []byte(`{"session_id":"x","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"curl -H 'Authorization: Bearer abcdefghijklmnopqrstu' x","api_key":"k"}}`)
	ev, _, err := Map("PreToolUse", in, env)
	if err != nil {
		t.Fatal(err)
	}
	f := Forward(ev, "PreToolUse", in)
	s := string(f.Payload)
	if f.Hook != "PreToolUse" || f.Event != local.EventTool || strings.Contains(s, "abcdefghijklmnopqrstu") || !strings.Contains(s, `"api_key":null`) {
		t.Fatalf("forward = %+v %s", f, s)
	}
	big := []byte(`{"x":"` + strings.Repeat("a", wire.MaxHookPayload) + `"}`)
	if Forward(ev, "PreToolUse", big).Payload != nil || Forward(ev, "PreToolUse", []byte("{bad")).Payload != nil {
		t.Fatal("oversized or malformed payload forwarded")
	}
	b, _ := json.Marshal(Forward(ev, "PreToolUse", in))
	var back wire.HookEvent
	if json.Unmarshal(b, &back) != nil || back.Event != local.EventTool {
		t.Fatal("a forward must still decode as a plain hook event")
	}
}

func TestExtraEventsForwardOnly(t *testing.T) {
	for _, ev := range []string{"SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "TaskCreated", "TaskCompleted"} {
		got, _, err := Map(ev, []byte(`{"session_id":"x"}`), map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"})
		if err != nil || got.Event != local.EventHarness {
			t.Errorf("%s = %+v %v", ev, got, err)
		}
	}
}

func TestSynthesize(t *testing.T) {
	f := wire.HookForward{HookEvent: wire.HookEvent{Agent: "claude", Event: local.EventTool, Cwd: "/p",
		Data: wire.HookData{Tool: "Bash", AgentSessionID: "abc"}}}
	name, p := Synthesize(f)
	if name != "PreToolUse" || !strings.Contains(string(p), `"tool_name":"Bash"`) || !strings.Contains(string(p), `"session_id":"abc"`) {
		t.Fatalf("synthesize = %s %s", name, p)
	}
	if name, _ := Synthesize(wire.HookForward{HookEvent: wire.HookEvent{Event: local.EventMeta}}); name != "" {
		t.Fatal("a status line cannot be rebuilt")
	}
	f.Payload, f.Hook = json.RawMessage(`{"a":1}`), "Stop"
	if name, p := Synthesize(f); name != "Stop" || string(p) != `{"a":1}` {
		t.Fatal("a real payload must pass through")
	}
}
