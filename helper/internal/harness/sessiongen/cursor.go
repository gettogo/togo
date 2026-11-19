package sessiongen

import "fmt"

// Cursor writes the session of Cursor CLI 2026.11: its hooks
// (~/.cursor/hooks.json, `togo hook cursor <event>`), snake_case keys
// under camelCase event names. It keeps no log Togo reads.
//
// Events and the payload fields the adapter reads (dialect.go Normalize
// renames conversation_id to session_id, tool_output to tool_response,
// error_message to error_details, text to last_assistant_message,
// subagent_id and subagent_type to agent_id and agent_type):
//
//	sessionStart        session_id (mapper.go:197, learn at :322)
//	beforeSubmitPrompt  prompt (mapper.go:202); model is learned from it
//	preToolUse          tool_name, tool_input, tool_use_id (mapper.go:218)
//	postToolUse         tool_use_id, tool_name, tool_output (mapper.go:243)
//	postToolUseFailure  tool_use_id, error_message (mapper.go:243)
//	afterAgentResponse  text: one finished reply; a turn may have several
//	                    (mapper.go:273)
//	stop                status: completed, aborted (interrupted) or error
//	                    (failed) (dialect.go StopStatus, mapper.go:280)
//	subagentStart/Stop  subagent_id, subagent_type (rows)
//
// AskQuestion fires no hook (cursor/ask.go), so a question never reaches
// the chat from hooks, and no hook says Cursor asks permission: the
// profile writes neither.
//
// Shapes: hookstyle/testdata/cursor.jsonl and cursor/testdata/
// hooks-real-shell.jsonl (Cursor 2026.11.05). subagentStart and
// subagentStop have no capture: hookstyle/mapper_turns_test.go:124 quotes
// subagent_id and subagent_type.
type Cursor struct {
	session  string
	sessions []string
	model    string
	gen      string
}

func (c *Cursor) Name() string { return "cursor" }

func (c *Cursor) Open(g *Gen) error { return c.start(g, "") }

func (c *Cursor) start(g *Gen, resume string) error {
	if resume != "" {
		c.session = resume
	} else {
		c.session = uuid(g)
		c.sessions = append(c.sessions, c.session)
	}
	g.L.Mains = append(g.L.Mains, "cursor:"+c.session)
	c.gen = c.session
	g.call(c.session, "sessionStart", c.base(g, "sessionStart", map[string]any{"is_background_agent": false}))
	return nil
}

func (c *Cursor) base(g *Gen, event string, p map[string]any) map[string]any {
	p["conversation_id"] = c.session
	p["generation_id"] = c.gen
	p["model"] = firstNonEmpty(c.model, "composer-2.5")
	p["session_id"] = c.session
	p["hook_event_name"] = event
	p["cursor_version"] = "2026.11.05-86047df"
	p["workspace_roots"] = []any{g.Cwd}
	p["transcript_path"] = nil
	return p
}

func (c *Cursor) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	if t.Compact || t.Jobs > 0 || h.Approve > 0 || h.Ask != nil {
		return fmt.Errorf("sessiongen: cursor's hooks carry no compaction, job, permission or question")
	}
	switch {
	case h.Resume && len(c.sessions) > 1:
		if err := c.start(g, c.sessions[len(c.sessions)-2]); err != nil {
			return err
		}
	case t.New:
		if err := c.start(g, ""); err != nil {
			return err
		}
	}
	if h.Model != "" {
		c.model = h.Model
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	c.gen = uuid(g)
	g.call(c.session, "beforeSubmitPrompt", c.base(g, "beforeSubmitPrompt", map[string]any{"prompt": prompt, "attachments": []any{}}))
	if h.Steer {
		g.L.Truth.Prompts++
		g.call(c.session, "beforeSubmitPrompt", c.base(g, "beforeSubmitPrompt", map[string]any{
			"prompt": fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts, g.Text(30)), "attachments": []any{}}))
	}
	for i := 0; i < t.Tools; i++ {
		id := uuid(g)
		name, in := "Shell", map[string]any{"command": "echo " + g.Hex(4), "cwd": g.Cwd, "timeout": 30000}
		if i%3 == 1 {
			name, in = "Read", map[string]any{"file_path": g.Cwd + "/file" + g.Hex(2) + ".go"}
		}
		g.call(c.session, "preToolUse", c.base(g, "preToolUse", map[string]any{"tool_name": name, "tool_input": in, "tool_use_id": id, "cwd": g.Cwd}))
		if h.Pending && i == t.Tools-1 && t.Agents == nil {
			return nil
		}
		if i < h.Fail {
			g.call(c.session, "postToolUseFailure", c.base(g, "postToolUseFailure", map[string]any{"tool_name": name, "tool_input": in,
				"error_message": "File not found", "failure_type": "error", "duration": 3, "tool_use_id": id, "is_interrupt": false}))
			continue
		}
		g.call(c.session, "postToolUse", c.base(g, "postToolUse", map[string]any{"tool_name": name, "tool_input": in,
			"tool_output": g.Text(max(h.Large, t.Output, 1)), "duration": 41, "tool_use_id": id, "cwd": g.Cwd}))
	}
	if t.Agents != nil {
		if t.Agents.Steps > 0 {
			return fmt.Errorf("sessiongen: cursor's hooks carry no step of a subagent")
		}
		var ids []string
		for i := 0; i < t.Agents.Size; i++ {
			id := "sub-" + g.Hex(8)
			ids = append(ids, id)
			g.call(c.session, "subagentStart", c.base(g, "subagentStart", map[string]any{"subagent_id": id, "subagent_type": "explore"}))
		}
		if !t.Agents.Background {
			for _, id := range ids {
				g.call(c.session, "subagentStop", c.base(g, "subagentStop", map[string]any{"subagent_id": id, "subagent_type": "explore"}))
			}
		}
		for _, id := range ids {
			g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: id, Settled: !t.Agents.Background})
		}
	}
	stop := func(status, end string) {
		g.call(c.session, "stop", c.base(g, "stop", map[string]any{"status": status, "loop_count": 0, "input_tokens": 1200, "output_tokens": 14}))
		g.ended(end)
	}
	switch {
	case t.Interrupt:
		stop("aborted", "interrupted")
	case t.Error:
		stop("error", "failed")
	case h.NoStop:
	default:
		g.call(c.session, "afterAgentResponse", c.base(g, "afterAgentResponse", map[string]any{"text": g.Text(max(t.Say, 1)), "input_tokens": 1200, "output_tokens": 14}))
		stop("completed", "ok")
	}
	if t.Exit {
		// No capture shows a sessionEnd payload, so none is written: the
		// exit is the process's, which the hub reports.
		g.L.Truth.Exited = true
	}
	return nil
}
