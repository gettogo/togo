package sessiongen

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Droid writes the session of Factory Droid 1.5: Claude-shaped hooks
// (~/.factory/hooks.json, `togo hook droid <event>`) and the session
// transcript the Stop hook names.
//
// Events and the payload fields the adapter reads (the payload is read as
// is; Droid's names are Claude Code's, dialect.go has no key changes):
//
//	SessionStart       session_id, source ("startup", "resume"),
//	                   permission_mode (mapper.go:197, learn at :322)
//	UserPromptSubmit   prompt (mapper.go:202)
//	PreToolUse         tool_name, tool_input; Droid sends no tool_use_id,
//	                   so a result goes to the oldest running call of the
//	                   same tool (mapper.go:218, callID at :403). AskUser
//	                   and ExitSpecMode become questions (droid/ask.go)
//	PostToolUse        tool_name, tool_response (text for AskUser)
//	Stop               transcript_path: read for the turn's last
//	                   agent_turn_outcome (droidPrep, failures.go:50); a
//	                   failed outcome makes it StopFailure. The hook
//	                   carries no reply text
//	Notification       message: "stopped by user" ends the turn interrupted
//	                   (droidPrep); the rest is read by the summary only
//	SessionEnd         ends the turn like Stop (mapper.go:280)
//
// Droid has no SubagentStart hook (droid.yaml lists SubagentStop only) and
// no capture holds a SubagentStop payload, so the profile writes no
// subagent and returns an error for a script that has one.
//
// The transcript, ~/.factory/sessions/<cwd key>/<session id>.jsonl, is read
// only by droidOutcome (failures.go:84): message records (id, message.role,
// message.visibility, message.content[].text) and agent_turn_outcome
// (turnId, reason).
//
// Shapes: session_start and message records are from
// hookstyle/testdata/droid-transcript-unreachable.jsonl (Droid 1.5.0);
// SessionStart, Stop, SessionEnd, PreToolUse, PostToolUse and the AskUser
// Notification from droid/testdata/hooks-real-exec.jsonl and
// hooks-ask.jsonl; UserPromptSubmit from hookstyle/testdata/droid.jsonl;
type Droid struct {
	session    string
	sessions   []string
	transcript string
	started    bool
	lastPrompt string
}

func (d *Droid) Name() string { return "droid" }

func (d *Droid) Open(g *Gen) error { return d.start(g, "startup", "") }

func (d *Droid) start(g *Gen, source, resume string) error {
	if resume != "" {
		d.session = resume
	} else {
		d.session = uuid(g)
		d.sessions = append(d.sessions, d.session)
	}
	key := nonAlnum.ReplaceAllString(filepath.Clean(g.Cwd), "-")
	d.transcript = filepath.Join(g.Home, ".factory", "sessions", key, d.session+".jsonl")
	g.L.Mains = append(g.L.Mains, d.transcript)
	d.started = false
	g.call(d.session, "SessionStart", d.base(g, "SessionStart", map[string]any{"source": source}))
	return nil
}

func (d *Droid) base(g *Gen, event string, p map[string]any) map[string]any {
	p["session_id"] = d.session
	p["transcript_path"] = d.transcript
	p["cwd"] = g.Cwd
	p["permission_mode"] = "auto-low"
	p["hook_event_name"] = event
	return p
}

func (d *Droid) line(g *Gen, rec map[string]any) FileLine {
	if rec["id"] == nil && rec["type"] != "agent_turn_outcome" {
		rec["id"] = uuid(g)
	}
	if rec["timestamp"] == nil && rec["type"] == "message" {
		rec["timestamp"] = stamp(g.now)
	}
	return FileLine{Path: d.transcript, Record: rec}
}

func (d *Droid) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	if t.Compact || t.Jobs > 0 {
		return fmt.Errorf("sessiongen: droid's hooks carry no compaction or background job")
	}
	switch {
	case h.Resume && len(d.sessions) > 1:
		if err := d.start(g, "resume", d.sessions[len(d.sessions)-2]); err != nil {
			return err
		}
	case t.New:
		if err := d.start(g, "startup", ""); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	var writes []FileLine
	if !d.started {
		d.started = true
		writes = append(writes, d.line(g, map[string]any{"type": "session_start", "title": "session", "owner": "user",
			"version": 2, "cwd": g.Cwd, "hostId": "host", "isSessionTitleManuallySet": false}))
	}
	pid := uuid(g)
	d.lastPrompt = pid
	writes = append(writes, d.line(g, map[string]any{"type": "message", "id": pid, "message": map[string]any{
		"role": "user", "content": []any{map[string]any{"type": "text", "text": prompt}}, "interactionMode": "auto"}}))
	g.call(d.session, "UserPromptSubmit", d.base(g, "UserPromptSubmit", map[string]any{"prompt": prompt}), writes...)
	if h.Steer {
		g.L.Truth.Prompts++
		g.call(d.session, "UserPromptSubmit", d.base(g, "UserPromptSubmit", map[string]any{"prompt": fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts, g.Text(30))}))
	}
	for i := 0; i < t.Tools; i++ {
		name, in := "Execute", map[string]any{"command": "echo " + g.Hex(4), "riskLevel": "low", "summary": "Run a command"}
		out := any(g.Text(max(h.Large, t.Output, 1)) + "\n\n[Process exited with code 0]")
		if i%3 == 1 {
			name, in, out = "Edit", map[string]any{"file_path": g.Cwd + "/file" + g.Hex(2) + ".go", "old_str": "a", "new_str": "b"}, map[string]any{"success": true}
		}
		g.call(d.session, "PreToolUse", d.base(g, "PreToolUse", map[string]any{"tool_name": name, "tool_input": in}))
		if i < h.Approve {
			g.call(d.session, "Notification", d.base(g, "Notification", map[string]any{"message_id": uuid(g),
				"message": "Factory CLI needs permission to execute 1 tool(s)", "notification_type": "permission_prompt"}))
		}
		if h.Pending && i == t.Tools-1 && h.Ask == nil && t.Agents == nil {
			return nil
		}
		// No capture shows how Droid reports a failed tool, so h.Fail is
		// not written: every tool ends as the captures do.
		g.call(d.session, "PostToolUse", d.base(g, "PostToolUse", map[string]any{"tool_name": name, "tool_input": in, "tool_response": out}))
	}
	if t.Agents != nil {
		return fmt.Errorf("sessiongen: droid has no SubagentStart hook and no capture of its SubagentStop")
	}
	if a := h.Ask; a != nil {
		if err := d.ask(g, a, h.Pending); err != nil {
			return err
		}
		if h.Pending {
			return nil
		}
	}
	stop := func(end string, extra ...FileLine) {
		g.call(d.session, "Stop", d.base(g, "Stop", map[string]any{"message_id": uuid(g), "stop_hook_active": false,
			"tool_execution_count": t.Tools, "elapsed_time": 8}), extra...)
		g.ended(end)
	}
	switch {
	case t.Interrupt:
		g.call(d.session, "Notification", d.base(g, "Notification", map[string]any{"message_id": uuid(g),
			"message": "Agent stopped by user and is waiting for input", "notification_type": "idle_prompt"}))
		g.ended("interrupted")
	case t.Error:
		words := firstNonEmpty(h.ErrorText, "Unable to reach the model provider. Check your network connection and try again.")
		stop("failed", d.line(g, map[string]any{"type": "message", "parentId": pid, "message": map[string]any{"role": "user", "visibility": "user_only",
			"content": []any{map[string]any{"type": "text", "text": words}}}}),
			d.line(g, map[string]any{"type": "agent_turn_outcome", "turnId": pid, "reason": "model_provider_unreachable", "resultKind": "text"}))
	case h.NoStop:
	default:
		stop("ok", d.line(g, map[string]any{"type": "agent_turn_outcome", "turnId": pid, "reason": "completed", "resultKind": "text"}))
	}
	if t.Exit {
		g.L.Truth.Exited = true
		g.call(d.session, "SessionEnd", d.base(g, "SessionEnd", map[string]any{"message_id": uuid(g), "reason": "other",
			"session_duration_ms": 1454.7, "message_count": 5}))
	}
	return nil
}

// ask writes Droid's AskUser (the plain-text questionnaire of
// testdata/hooks-ask.jsonl) or, with a plan, ExitSpecMode.
func (d *Droid) ask(g *Gen, a *HookAsk, open bool) error {
	tool, in := "AskUser", map[string]any{}
	var resp any
	if a.Plan != "" {
		tool, in = "ExitSpecMode", map[string]any{"plan": a.Plan, "title": "Plan"}
		resp = `{"approved":true}`
		if a.Cancel {
			resp = `{"approved":false}`
		}
	} else {
		var b strings.Builder
		b.WriteString("1. [question] " + a.Prompt + "\n[topic] Topic")
		for _, o := range a.Options {
			b.WriteString("\n[option] " + o)
		}
		in["questionnaire"] = b.String()
		resp = "1. [question] " + a.Prompt + "\n[answer] " + a.Answer
		if a.Cancel {
			resp = "User cancelled AskUser"
		}
	}
	g.call(d.session, "PreToolUse", d.base(g, "PreToolUse", map[string]any{"tool_name": tool, "tool_input": in}))
	if open {
		return nil
	}
	g.call(d.session, "PostToolUse", d.base(g, "PostToolUse", map[string]any{"tool_name": tool, "tool_input": in, "tool_response": resp}))
	return nil
}
