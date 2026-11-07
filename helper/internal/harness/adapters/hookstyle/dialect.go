// Package hookstyle reads hook payloads shaped like Claude Code's, which
// several harnesses share: Droid sends that shape as is,
// Mistral Vibe sends it for hooks that use its claude_code protocol, and
// Copilot CLI sends a camelCase variant with its own event names.
//
// Normalize turns each dialect into Claude Code's event names and
// snake_case keys. The Mapper reads normalized payloads into session state,
// chat items and mission rows. Reduce and Decision run in the hook process
// (`togo hook <agent> <event>`).
package hookstyle

import (
	"encoding/json"
	"errors"
)

// Canonical event names (Claude Code's), plus the few events only one
// harness has.
const (
	SessionStart       = "SessionStart"
	SessionEnd         = "SessionEnd"
	UserPromptSubmit   = "UserPromptSubmit"
	PreToolUse         = "PreToolUse"
	PostToolUse        = "PostToolUse"
	PostToolUseFailure = "PostToolUseFailure"
	PermissionRequest  = "PermissionRequest"
	Notification       = "Notification"
	Stop               = "Stop"
	StopFailure        = "StopFailure"
	SubagentStart      = "SubagentStart"
	SubagentStop       = "SubagentStop"
	PreCompact         = "PreCompact"
	PostCompact        = "PostCompact"
	TaskCreated        = "TaskCreated"
	TaskCompleted      = "TaskCompleted"
	// ErrorOccurred is Copilot's errorOccurred: an error the session may
	// survive, so it is not StopFailure.
	ErrorOccurred = "ErrorOccurred"
)

// Approval answer shapes a blocking hook prints.
const (
	// AnswerPermissionRequest is Claude Code's PermissionRequest output:
	// hookSpecificOutput.decision.behavior allow or deny.
	AnswerPermissionRequest = "permission_request"
	// AnswerPermissionDecision is a top-level permissionDecision allow or
	// deny, Copilot CLI's hook output.
	AnswerPermissionDecision = "permission_decision"
)

// Dialect is one harness's variant of the hook shape.
type Dialect struct {
	// Agent is the Togo agent id, the <agent> of `togo hook <agent>`.
	Agent string
	// Events maps the harness's own event names to canonical ones. Names
	// not listed pass through.
	Events map[string]string
	// Answer is how a blocking PermissionRequest hook answers: one of the
	// Answer* shapes, or "" when the harness has no hook that can.
	Answer string
	// Modes maps the harness's permission_mode values to the adapter's
	// mode ids where they differ.
	Modes map[string]string
}

// Dialects by agent id.
var dialects = map[string]Dialect{
	"droid": {Agent: "droid"},
	"vibe": {Agent: "vibe", Events: map[string]string{
		"pre_tool": PreToolUse, "post_tool": PostToolUse, "post_agent": Stop,
	}},
	"copilot": {Agent: "copilot", Answer: AnswerPermissionDecision, Events: map[string]string{
		"sessionStart": SessionStart, "sessionEnd": SessionEnd, "userPromptSubmitted": UserPromptSubmit,
		"preToolUse": PreToolUse, "postToolUse": PostToolUse, "postToolUseFailure": PostToolUseFailure,
		"permissionRequest": PermissionRequest, "agentStop": Stop, "subagentStart": SubagentStart,
		"subagentStop": SubagentStop, "errorOccurred": ErrorOccurred, "preCompact": PreCompact,
		"notification": Notification,
	}},
}

// For returns the dialect of an agent.
func For(agent string) (Dialect, bool) {
	d, ok := dialects[agent]
	return d, ok
}

// Agents lists the agents with a dialect.
func Agents() []string { return []string{"droid", "vibe", "copilot"} }

// Canonical returns the canonical name of one of the dialect's events.
func (d Dialect) Canonical(event string) string {
	if c, ok := d.Events[event]; ok {
		return c
	}
	return event
}

// keyAliases maps camelCase keys to Claude Code's snake_case ones. A key
// already present in snake_case wins.
var keyAliases = map[string]string{
	"sessionId":            "session_id",
	"hookEventName":        "hook_event_name",
	"transcriptPath":       "transcript_path",
	"permissionMode":       "permission_mode",
	"toolName":             "tool_name",
	"toolArgs":             "tool_input",
	"toolInput":            "tool_input",
	"toolResult":           "tool_response",
	"toolResponse":         "tool_response",
	"toolUseId":            "tool_use_id",
	"toolCallId":           "tool_use_id",
	"lastAssistantMessage": "last_assistant_message",
	"agentId":              "agent_id",
	"agentType":            "agent_type",
	"initialPrompt":        "prompt",
	"notificationType":     "notification_type",
	"workingDirectory":     "cwd",
	"agentName":            "agent_type",
	"agentDisplayName":     "agent_type",
}

// ErrPayload is returned for a payload that is not a JSON object. It never
// quotes the payload.
var ErrPayload = errors.New("hookstyle: payload is not a JSON object")

// Normalize returns the canonical event name and the payload in Claude
// Code's shape: snake_case keys, tool_input as an object when it arrived
// as JSON text, hook_event_name set to the canonical name.
func Normalize(d Dialect, event string, payload []byte) (string, json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(payload, &obj) != nil || obj == nil {
		return "", nil, ErrPayload
	}
	canon := d.Canonical(event)
	for from, to := range keyAliases {
		v, ok := obj[from]
		if !ok {
			continue
		}
		delete(obj, from)
		if _, has := obj[to]; !has {
			obj[to] = v
		}
	}
	// Copilot sends tool arguments as JSON text.
	if raw, ok := obj["tool_input"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil && json.Valid([]byte(s)) {
			obj["tool_input"] = json.RawMessage(s)
		}
	}
	// Copilot's errorOccurred carries {error: {message}} or a plain string.
	if canon == ErrorOccurred {
		var e struct {
			Message string `json:"message"`
		}
		var str string
		switch {
		case json.Unmarshal(obj["error"], &e) == nil && e.Message != "":
			obj["error_details"], _ = json.Marshal(e.Message)
		case json.Unmarshal(obj["error"], &str) == nil && str != "":
			obj["error_details"], _ = json.Marshal(str)
			delete(obj, "error")
		}
	}
	// Copilot's subagent hooks name the agent but carry no id: the tool
	// call that started it, else its name, keys the agent row.
	if canon == SubagentStart || canon == SubagentStop {
		if _, ok := obj["agent_id"]; !ok {
			for _, k := range []string{"tool_use_id", "agent_type"} {
				if v, ok := obj[k]; ok {
					obj["agent_id"] = v
					break
				}
			}
		}
	}
	if len(d.Modes) > 0 {
		var mode string
		if json.Unmarshal(obj["permission_mode"], &mode) == nil {
			if id, ok := d.Modes[mode]; ok {
				obj["permission_mode"], _ = json.Marshal(id)
			}
		}
	}
	obj["hook_event_name"], _ = json.Marshal(canon)
	out, err := json.Marshal(obj)
	if err != nil {
		return "", nil, ErrPayload
	}
	return canon, out, nil
}
