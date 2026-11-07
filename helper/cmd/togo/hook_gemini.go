package main

import (
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	forwardAdapters["gemini"] = forwardAdapter{
		reduce: geminiHook,
		// Gemini's hooks are read for state only; approvals stay in the
		// terminal.
		blocks:   func(string) bool { return false },
		decision: func(string) []byte { return nil },
	}
}

// geminiEvents maps Gemini CLI's hook names onto the hub's events. Hooks
// the hub has no use for still reach the harness adapter.
var geminiEvents = map[string]string{
	"SessionStart": local.EventStart, "BeforeAgent": local.EventPrompt, "AfterAgent": local.EventStop,
	"BeforeTool": local.EventTool, "AfterTool": local.EventToolDone, "Notification": local.EventNotify,
	"PreCompress": local.EventHarness, "SessionEnd": local.EventEnd,
}

// geminiHook reduces a Gemini CLI hook payload to the hub's summary and
// forwards the redacted payload to the harness adapter's hook feed.
func geminiHook(event string, payload []byte, env map[string]string) (wire.HookForward, error) {
	kind, ok := geminiEvents[event]
	if !ok {
		return wire.HookForward{}, errors.New("unknown gemini hook event")
	}
	var p struct {
		Session string `json:"session_id"`
		Cwd     string `json:"cwd"`
		Tool    string `json:"tool_name"`
		Message string `json:"message"`
	}
	if len(payload) > 0 && json.Unmarshal(payload, &p) != nil {
		return wire.HookForward{}, errors.New("gemini hook payload is not JSON")
	}
	ev := wire.HookEvent{Agent: "gemini", Event: kind, SessionID: claude.SessionID(env), Cwd: clip(p.Cwd, 4096),
		Data: wire.HookData{Tool: clip(p.Tool, 64), Message: clip(p.Message, 512), AgentSessionID: clip(p.Session, 128)}}
	return claude.Forward(ev, event, payload), nil
}

// clip cuts s to at most n bytes on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}
