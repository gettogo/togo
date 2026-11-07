package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	forwardAdapters["auggie"] = forwardAdapter{
		reduce: func(event string, payload []byte, env map[string]string) (wire.HookForward, error) {
			ev, _, err := auggieHook(context.Background(), event, payload, env)
			if err != nil {
				return wire.HookForward{}, err
			}
			return claude.Forward(ev, event, payload), nil
		},
		// Auggie hooks can deny but not allow, so they never answer
		// approvals.
		blocks:   func(string) bool { return false },
		decision: func(string) []byte { return nil },
	}
}

// auggieEvents maps Auggie's hook names onto the hub's events.
var auggieEvents = map[string]string{
	"SessionStart": local.EventStart, "PromptSubmit": local.EventPrompt, "PreToolUse": local.EventTool,
	"PostToolUse": local.EventToolDone, "Notification": local.EventNotify, "Stop": local.EventStop,
	"SessionEnd": local.EventEnd,
}

// auggieHook reduces an Auggie hook payload to the hub's summary; the raw
// payload goes on to the harness adapter's hook feed.
func auggieHook(_ context.Context, event string, payload []byte, env map[string]string) (wire.HookEvent, string, error) {
	kind, ok := auggieEvents[event]
	if !ok {
		return wire.HookEvent{}, "", errors.New("unknown auggie hook event")
	}
	var p struct {
		Conversation string   `json:"conversation_id"`
		Roots        []string `json:"workspace_roots"`
		Tool         string   `json:"tool_name"`
		Message      string   `json:"notification_message"`
	}
	if len(payload) > 0 && json.Unmarshal(payload, &p) != nil {
		return wire.HookEvent{}, "", errors.New("auggie hook payload is not JSON")
	}
	ev := wire.HookEvent{Agent: "auggie", Event: kind, SessionID: claude.SessionID(env),
		Data: wire.HookData{Tool: p.Tool, Message: p.Message, AgentSessionID: p.Conversation}}
	if len(p.Roots) > 0 {
		ev.Cwd = p.Roots[0]
	}
	return ev, "", nil
}
