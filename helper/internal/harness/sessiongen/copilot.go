package sessiongen

import (
	"fmt"
	"path/filepath"
)

// Copilot writes the session of GitHub Copilot CLI 1.0.x: its hooks
// (~/.copilot/hooks/togo.json, one `togo hook copilot <event>` process per
// event) and the log it keeps per session,
// ~/.copilot/session-state/<session id>/events.jsonl.
//
// Events and the payload fields the adapter reads. The mapper reads the
// normalized payload (hookstyle/dialect.go Normalize: camelCase keys to
// snake_case, toolArgs and toolInput as the tool_input object):
//
//	sessionStart         sessionId, source ("new", "resume"), cwd
//	                     (mapper.go:197, learn at :322)
//	userPromptSubmitted  sessionId, prompt (mapper.go:202)
//	preToolUse           sessionId, toolCallId, toolName, toolArgs
//	                     (mapper.go:218; ask_user: copilot/ask.go)
//	permissionRequest    toolName, toolInput; it carries no toolCallId, so
//	                     the oldest running call of that tool is the one
//	                     (mapper.go:261, callID at :403)
//	postToolUse          toolCallId, toolName, toolResult.resultType and
//	                     .textResultForLlm (mapper.go:243, tools.go
//	                     ToolOutput)
//	agentStop            sessionId (mapper.go:280); the reply is not in it
//	sessionEnd           reason: user_exit and complete end the turn,
//	                     abort ends it interrupted, error ends it failed
//	                     (copilotPrep, failures.go:170)
//	errorOccurred        error.message (mapper.go:305)
//	subagentStart/Stop   agentName (dialect.go Normalize; rows in
//	                     hookstyle/rows.go). Both carry only the name
//	                     here, as the tests quote them: a start that
//	                     carried toolCallId would key its row by it, and a
//	                     stop without one would not find it
//	notification         read by the summary only; the chat ignores it
//
// What the agent said is not in a hook. The mapper reads it from
// events.jsonl (copilot/replies.go:42): assistant.message records with
// data.content, data.toolRequests[].toolCallId and no
// data.parentToolCallId. The agent writes the message that requests a
// tool just before the tool's preToolUse hook, and the final message
// before agentStop; the profile attaches each line to the hook it
// precedes.
//
// Shapes: sessionStart, userPromptSubmitted, preToolUse, postToolUse,
// permissionRequest, notification, agentStop and the log's records are
// from Copilot 1.0.108 captures (copilot/testdata/hooks-ask.jsonl,
// ask.events.jsonl). sessionEnd, errorOccurred, subagentStart and
// subagentStop have no capture: their fields are those hookstyle's tests
// quote (hookstyle/testdata/copilot.jsonl, dialect_more_test.go,
// failures_test.go).
type Copilot struct {
	session  string
	sessions []string
	log      string
	prev     map[string]any
}

func (c *Copilot) Name() string { return "copilot" }

func (c *Copilot) Open(g *Gen) error { return c.start(g, "new", "") }

func (c *Copilot) start(g *Gen, source, resume string) error {
	if resume != "" {
		c.session = resume
	} else {
		c.session = uuid(g)
		c.sessions = append(c.sessions, c.session)
	}
	c.log = filepath.Join(g.Home, ".copilot", "session-state", c.session, "events.jsonl")
	g.L.Mains = append(g.L.Mains, c.log)
	g.call(c.session, "sessionStart", c.base(g, map[string]any{"source": source, "initialPrompt": ""}))
	return nil
}

// base adds the keys every payload of a session carries.
func (c *Copilot) base(g *Gen, p map[string]any) map[string]any {
	p["sessionId"] = c.session
	p["timestamp"] = ms(g.now)
	p["cwd"] = g.Cwd
	return p
}

func (c *Copilot) logLine(g *Gen, typ string, data map[string]any) FileLine {
	return FileLine{Path: c.log, Record: map[string]any{"type": typ, "data": data, "id": uuid(g),
		"timestamp": stamp(g.now), "parentId": uuid(g)}}
}

func (c *Copilot) assistant(g *Gen, content string, calls ...map[string]any) FileLine {
	reqs := []any{}
	for _, r := range calls {
		reqs = append(reqs, r)
	}
	return c.logLine(g, "assistant.message", map[string]any{"messageId": uuid(g), "originatingMessageId": uuid(g),
		"model": "claude-sonnet-5.5", "providerId": "copilot", "content": content, "toolRequests": reqs,
		"interactionId": uuid(g), "turnId": "0", "rte": true, "apiCallId": "msg_" + g.Hex(24)})
}

func (c *Copilot) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	if t.Compact || t.Jobs > 0 {
		return fmt.Errorf("sessiongen: copilot writes no compaction or background job record")
	}
	switch {
	case h.Resume && len(c.sessions) > 1:
		if err := c.start(g, "resume", c.sessions[len(c.sessions)-2]); err != nil {
			return err
		}
	case t.New:
		if err := c.start(g, "new", ""); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	g.call(c.session, "userPromptSubmitted", c.base(g, map[string]any{"prompt": prompt}),
		c.logLine(g, "user.message", map[string]any{"content": prompt}))
	if h.Steer {
		g.L.Truth.Prompts++
		g.call(c.session, "userPromptSubmitted", c.base(g, map[string]any{"prompt": fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts, g.Text(30))}))
	}
	for i := 0; i < t.Tools; i++ {
		last := h.Pending && i == t.Tools-1 && h.Ask == nil && t.Agents == nil
		name, args := "bash", map[string]any{"command": "echo " + g.Hex(4), "description": "run"}
		if i%3 == 1 {
			name, args = "view", map[string]any{"path": g.Cwd + "/file" + g.Hex(2) + ".go"}
		}
		id := "toolu_" + g.Hex(24)
		narration := ""
		if i%3 == 0 {
			narration = g.Text(30)
		}
		g.call(c.session, "preToolUse", c.base(g, map[string]any{"toolCallId": id, "toolName": name, "toolArgs": args}),
			c.assistant(g, narration, map[string]any{"toolCallId": id, "name": name, "arguments": args, "type": "function",
				"intentionSummary": "Running", "toolTitle": "Running"}))
		if i < h.Approve {
			g.call(c.session, "permissionRequest", c.base(g, map[string]any{"hookName": "permissionRequest", "toolName": name,
				"toolInput": args, "permissionSuggestions": []any{}}))
		}
		if last {
			return nil
		}
		res := map[string]any{"resultType": "success", "textResultForLlm": g.Text(max(h.Large, t.Output, 1))}
		if i < h.Fail {
			res = map[string]any{"resultType": "failure", "textResultForLlm": "permission denied"}
		}
		g.call(c.session, "postToolUse", c.base(g, map[string]any{"toolCallId": id, "toolName": name, "toolArgs": args, "toolResult": res}))
	}
	if t.Agents != nil {
		if t.Agents.Steps > 0 {
			return fmt.Errorf("sessiongen: copilot's hooks carry no step of a subagent")
		}
		for i := 0; i < t.Agents.Size; i++ {
			g.call(c.session, "subagentStart", c.base(g, map[string]any{"agentName": "explore"}))
		}
		if !t.Agents.Background {
			for i := 0; i < t.Agents.Size; i++ {
				g.call(c.session, "subagentStop", c.base(g, map[string]any{"agentName": "explore"}))
			}
		}
		// The hooks name only the agent type, so the agents of one batch
		// are told apart by order: "explore", "explore#2", and so on.
		for i := 0; i < t.Agents.Size; i++ {
			task := "explore"
			if i > 0 {
				task = fmt.Sprintf("explore#%d", i+1)
			}
			g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: task, Settled: !t.Agents.Background})
		}
	}
	if a := h.Ask; a != nil {
		if err := c.ask(g, a, h.Pending); err != nil {
			return err
		}
		if h.Pending {
			return nil
		}
	}
	switch {
	case t.Error:
		g.call(c.session, "errorOccurred", map[string]any{"timestamp": ms(g.now), "cwd": g.Cwd, "error": map[string]any{
			"message": firstNonEmpty(h.ErrorText, "network timeout"), "name": "Error", "stack": "REDACTED"}})
		g.call(c.session, "sessionEnd", map[string]any{"timestamp": ms(g.now), "cwd": g.Cwd, "reason": "error", "error": firstNonEmpty(h.ErrorText, "network timeout")})
		g.ended("failed")
	case t.Interrupt:
		g.call(c.session, "sessionEnd", map[string]any{"timestamp": ms(g.now), "cwd": g.Cwd, "reason": "abort"})
		g.ended("interrupted")
	case h.NoStop:
	default:
		g.call(c.session, "agentStop", c.base(g, map[string]any{"transcriptPath": c.log, "stopReason": "end_turn", "stop_hook_active": false}),
			c.assistant(g, g.Text(max(t.Say, 1))))
		g.ended("ok")
	}
	if t.Exit {
		g.L.Truth.Exited = true
		g.call(c.session, "sessionEnd", map[string]any{"timestamp": ms(g.now), "cwd": g.Cwd, "reason": "user_exit"})
	}
	return nil
}

// ask writes Copilot's ask_user tool: one choice field (the capture in
// testdata/hooks-ask.jsonl). Unanswered leaves the call open.
func (c *Copilot) ask(g *Gen, a *HookAsk, open bool) error {
	id := "toolu_" + g.Hex(24)
	opts := make([]any, len(a.Options))
	for i, o := range a.Options {
		opts[i] = o
	}
	args := map[string]any{"message": a.Prompt, "requestedSchema": map[string]any{"properties": map[string]any{
		"choice": map[string]any{"type": "string", "title": "Choice", "enum": opts}}}}
	g.call(c.session, "preToolUse", c.base(g, map[string]any{"toolCallId": id, "toolName": "ask_user", "toolArgs": args}),
		c.assistant(g, "", map[string]any{"toolCallId": id, "name": "ask_user", "arguments": args, "type": "function",
			"intentionSummary": "Waiting for response", "toolTitle": "Asking user"}))
	g.call(c.session, "notification", c.base(g, map[string]any{"message": "Information requested", "title": "Information requested",
		"hook_event_name": "Notification", "notification_type": "elicitation_dialog"}))
	if open {
		return nil
	}
	text := "User responded: " + a.Answer
	if a.Cancel {
		text = "The user cancelled the request"
	}
	g.call(c.session, "postToolUse", c.base(g, map[string]any{"toolCallId": id, "toolName": "ask_user", "toolArgs": args,
		"toolResult": map[string]any{"resultType": "success", "textResultForLlm": text}}))
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
