// Package claude translates Claude Code hook and status line payloads into
// Togo hook events. Field names follow the Claude Code hooks reference
// (code.claude.com/docs/en/hooks: common input fields, PreToolUse,
// PermissionRequest, Notification, Stop, SessionStart) and the status line
// reference (code.claude.com/docs/en/statusline: model.display_name,
// context_window.used_percentage, cost.total_cost_usd). Only the fields the
// helper uses are read; the raw payload never leaves this package.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// MaxPayload bounds what a hook reads from stdin.
	MaxPayload = 1 << 20

	agentName   = "claude"
	maxTool     = 64
	maxCommand  = 1024
	maxMessage  = wire.MaxBody
	maxModel    = 80
	approveText = "Allow"
	denyText    = "Deny"
)

// Statusline is the pseudo event name for the status line command.
const Statusline = "statusline"

// Output is what the hook prints for Claude Code, apart from a blocking
// permission decision, which the caller builds with Decision.
type Output struct {
	// Text is printed to stdout; only the status line has any.
	Text string
}

type payload struct {
	SessionID        string          `json:"session_id"`
	Cwd              string          `json:"cwd"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	LastAssistant    string          `json:"last_assistant_message"`
	Model            json.RawMessage `json:"model"`
	Workspace        struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	ContextWindow struct {
		UsedPercentage *float64 `json:"used_percentage"`
	} `json:"context_window"`
	Cost struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

// ErrUnsupported is returned for events this adapter does not map.
var ErrUnsupported = errors.New("claude: unsupported event")

// Map reduces one hook invocation to a HookEvent. sessionId comes from
// TOGO_SESSION and is nil outside a managed session. Errors never quote the
// payload.
func Map(event string, stdin []byte, env map[string]string) (wire.HookEvent, Output, error) {
	if len(stdin) > MaxPayload {
		return wire.HookEvent{}, Output{}, errors.New("claude: payload too large")
	}
	var p payload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return wire.HookEvent{}, Output{}, errors.New("claude: malformed payload")
	}
	ev := wire.HookEvent{Agent: agentName, SessionID: SessionID(env), Cwd: p.Cwd}
	ev.Data.AgentSessionID = clean(p.SessionID, 128)
	var out Output
	switch event {
	case "UserPromptSubmit":
		ev.Event = local.EventPrompt
	case "PreToolUse":
		ev.Event, ev.Data.Tool = local.EventTool, clean(p.ToolName, maxTool)
	case "PostToolUse", "PostToolUseFailure":
		ev.Event, ev.Data.Tool = local.EventToolDone, clean(p.ToolName, maxTool)
	case "PermissionRequest":
		ev.Event, ev.Data.Tool = local.EventPermission, clean(p.ToolName, maxTool)
		what := target(p.ToolInput)
		ev.Data.Command = clean(what, maxCommand)
		ev.Data.Subject = detect.NewSubject(detect.SubjectKindForTool(p.ToolName), what)
	case "Notification":
		ev.Data.Message = clean(p.Message, maxMessage)
		switch p.NotificationType {
		case "permission_prompt":
			ev.Event = local.EventPermission
		case "idle_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input":
			ev.Event = local.EventAttention
		default:
			ev.Event = local.EventNotify
		}
	case "Stop":
		ev.Event, ev.Data.Message = local.EventStop, clean(p.LastAssistant, maxMessage)
	case "SessionStart":
		ev.Event, ev.Data.Model = local.EventStart, model(p.Model)
	case Statusline:
		ev.Event = local.EventMeta
		ev.Data.Model = model(p.Model)
		ev.Data.ContextPercent = percent(p.ContextWindow.UsedPercentage)
		if c := p.Cost.TotalCostUSD; c != nil && *c >= 0 {
			v := *c
			ev.Data.CostUSD = &v
		}
		if ev.Cwd == "" {
			ev.Cwd = p.Workspace.CurrentDir
		}
		out.Text = statusText(ev.Data)
	default:
		return wire.HookEvent{}, Output{}, fmt.Errorf("%w %q", ErrUnsupported, clean(event, 40))
	}
	return ev, out, nil
}

// SessionID returns the managed session id from TOGO_SESSION, or nil.
func SessionID(env map[string]string) *string {
	id := env["TOGO_SESSION"]
	if managed, err := session.ValidateID(id); err != nil || !managed {
		return nil
	}
	return &id
}

// AskFor builds the local.ask a blocking PermissionRequest hook sends.
func AskFor(ev wire.HookEvent, timeoutSec int) wire.AskParams {
	title := "Allow " + firstNonEmpty(ev.Data.Tool, "tool")
	if ev.Data.Command != "" {
		title += ": " + ev.Data.Command
	}
	return wire.AskParams{
		SessionID:  ev.SessionID,
		Title:      clean(title, wire.MaxTitle),
		Options:    []string{approveText, denyText},
		TimeoutSec: timeoutSec,
	}
}

// Decision returns the PermissionRequest hook output for an ask answer, or
// nil when the answer is neither allow nor deny, so Claude Code shows its
// own prompt.
func Decision(answer string) []byte {
	var d map[string]any
	switch answer {
	case approveText:
		d = map[string]any{"behavior": "allow"}
	case denyText, wire.DecisionDeny:
		d = map[string]any{"behavior": "deny", "message": "Denied from Togo"}
	default:
		return nil
	}
	b, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": d},
	})
	return b
}

// target names what a tool call touches without forwarding the rest of its
// input: the shell command, the file path for file tools, or the URL.
func target(raw json.RawMessage) string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		URL      string `json:"url"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	return firstNonEmpty(in.Command, firstNonEmpty(in.FilePath, in.URL))
}

// model reads either the SessionStart string or the status line object.
func model(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clean(s, maxModel)
	}
	var o struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return clean(firstNonEmpty(o.DisplayName, o.ID), maxModel)
	}
	return ""
}

func percent(p *float64) *float64 {
	if p == nil || *p < 0 || *p > 100 {
		return nil
	}
	v := *p
	return &v
}

func statusText(d wire.HookData) string {
	var parts []string
	if d.Model != "" {
		parts = append(parts, d.Model)
	}
	if d.ContextPercent != nil {
		parts = append(parts, fmt.Sprintf("%.0f%%", *d.ContextPercent))
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// clean trims s to n runes and drops control characters.
func clean(s string, n int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == n {
			break
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '�' {
			if r == '\n' || r == '\t' {
				r = ' '
			} else {
				continue
			}
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}
