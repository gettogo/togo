package claude

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness/redact"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Forward adds the hook's raw payload to the event for the harness
// adapter: redacted here, before it leaves the hook process, and dropped
// when it is larger than wire.MaxHookPayload or not JSON. event is the
// hook's own event name (PreToolUse, or Statusline).
func Forward(ev wire.HookEvent, event string, stdin []byte) wire.HookForward {
	f := wire.HookForward{HookEvent: ev, Hook: clean(event, 64)}
	if len(stdin) == 0 || len(stdin) > wire.MaxHookPayload || !json.Valid(stdin) {
		return f
	}
	red := redact.JSON(stdin)
	if len(red) <= wire.MaxHookPayload && string(red) != "null" {
		f.Payload = red
	}
	return f
}

// hookNames gives each reduced event the Claude Code hook it came from,
// for hooks forwarded without their payload.
var hookNames = map[string]string{
	local.EventPrompt: "UserPromptSubmit", local.EventTool: "PreToolUse", local.EventToolDone: "PostToolUse",
	local.EventPermission: "PermissionRequest", local.EventStop: "Stop", local.EventStart: "SessionStart",
	local.EventEnd: "SessionEnd", local.EventFailure: "StopFailure", local.EventUsageLimit: "StopFailure",
	local.EventAttention: "Notification", local.EventNotify: "Notification",
}

// Synthesize rebuilds a minimal Claude Code payload from a hook forwarded
// without one (an older togo hook command): the event name, the session
// ids and the tool. It returns the event name ("" for the status line,
// which it cannot rebuild) and the payload.
func Synthesize(f wire.HookForward) (string, json.RawMessage) {
	if len(f.Payload) > 0 {
		return f.Hook, f.Payload
	}
	if f.Event == local.EventMeta {
		return "", nil
	}
	name := f.Hook
	if name == "" {
		name = hookNames[f.Event]
	}
	if name == "" {
		return "", nil
	}
	p := map[string]any{"hook_event_name": name, "session_id": f.Data.AgentSessionID, "cwd": f.Cwd}
	if f.Data.Tool != "" {
		p["tool_name"] = f.Data.Tool
	}
	if f.Data.Model != "" {
		p["model"] = f.Data.Model
	}
	if f.Event == local.EventUsageLimit {
		p["error"] = "rate_limit"
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", nil
	}
	return name, b
}
