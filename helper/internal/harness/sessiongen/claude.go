package sessiongen

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Claude writes Claude Code's transcripts: ~/.claude/projects/<cwd key>/
// <session id>.jsonl, with each subagent in <transcript without .jsonl>/
// subagents/agent-<agent id>.jsonl. Claude Code learns of the transcript
// through its SessionStart hook, which the test delivers; a subagent's
// file records no exit of its own. Shapes follow adapters/claude/testdata.
type Claude struct {
	session string
	main    string
	jobs    int
}

func (c *Claude) Name() string { return "claude" }

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func (c *Claude) Open(g *Gen) error {
	c.session = fmt.Sprintf("%s-%s-4%s-8%s-%s", g.Hex(8), g.Hex(4), g.Hex(3), g.Hex(3), g.Hex(12))
	c.main = filepath.Join(g.Home, ".claude", "projects", nonAlnum.ReplaceAllString(filepath.Clean(g.Cwd), "-"), c.session+".jsonl")
	g.L.Mains = append(g.L.Mains, c.main)
	return nil
}

// entry adds uuid, timestamp, sessionId and isSidechain and appends to
// path; agent is the subagent's id, "" in the main transcript.
func (c *Claude) entry(g *Gen, path, agent string, e map[string]any) error {
	at := g.Tick()
	e["uuid"] = fmt.Sprintf("%s-%s-%s", g.Hex(8), g.Hex(4), g.Hex(12))
	e["timestamp"] = stamp(at)
	e["sessionId"] = c.session
	e["isSidechain"] = agent != ""
	if agent != "" {
		e["agentId"] = agent
	}
	return g.Emit(path, at, e)
}

func (c *Claude) user(content any) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
}

func (c *Claude) assistant(g *Gen, blocks ...any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"id": "msg_" + g.Hex(16), "role": "assistant",
		"model": "claude-opus-5-5", "content": blocks,
		"usage": map[string]any{"input_tokens": 2, "cache_creation_input_tokens": 1200 + g.Intn(800), "cache_read_input_tokens": 72659}}}
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func (c *Claude) result(id, out string, use map[string]any) map[string]any {
	e := c.user([]any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": out, "is_error": false}})
	if use != nil {
		e["toolUseResult"] = use
	}
	return e
}

func (c *Claude) turnEnd(g *Gen) error {
	return c.entry(g, c.main, "", map[string]any{"type": "system", "subtype": "turn_duration", "durationMs": 8000, "isMeta": false})
}

func (c *Claude) Turn(g *Gen, t Turn) error {
	if t.New {
		if err := c.Open(g); err != nil {
			return err
		}
	}
	if t.Compact {
		if err := c.entry(g, c.main, "", map[string]any{"type": "system", "subtype": "compact_boundary", "isMeta": false,
			"content": "Conversation compacted", "compactMetadata": map[string]any{"trigger": "auto"}}); err != nil {
			return err
		}
		sum := c.user(g.Text(200))
		sum["isCompactSummary"] = true
		if err := c.entry(g, c.main, "", sum); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	if err := c.entry(g, c.main, "", c.user(prompt)); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		id := "toolu_" + g.Hex(24)
		if err := c.entry(g, c.main, "", c.assistant(g, toolUse(id, "Bash", map[string]any{"command": "echo " + g.Hex(4)}))); err != nil {
			return err
		}
		if err := c.entry(g, c.main, "", c.result(id, g.Text(max(t.Output, 1)), nil)); err != nil {
			return err
		}
	}
	var settle []string // task ids reported once the turn has ended
	for i := 0; i < t.Jobs; i++ {
		c.jobs++
		id, task := "toolu_"+g.Hex(24), "b"+g.Hex(8)
		g.L.Truth.Jobs = append(g.L.Truth.Jobs, task)
		if err := c.entry(g, c.main, "", c.assistant(g, toolUse(id, "Bash", map[string]any{"command": "sleep 30", "run_in_background": true}))); err != nil {
			return err
		}
		if err := c.entry(g, c.main, "", c.result(id, "Command running in background", map[string]any{"backgroundTaskId": task})); err != nil {
			return err
		}
		settle = append(settle, task)
	}
	if t.Agents != nil {
		ids, err := c.batch(g, *t.Agents)
		if err != nil {
			return err
		}
		settle = append(settle, ids...)
	}
	if t.Interrupt {
		if err := c.entry(g, c.main, "", c.user("[Request interrupted by user]")); err != nil {
			return err
		}
	} else {
		if err := c.entry(g, c.main, "", c.assistant(g, map[string]any{"type": "text", "text": g.Text(max(t.Say, 1))})); err != nil {
			return err
		}
		if err := c.turnEnd(g); err != nil {
			return err
		}
	}
	for _, k := range g.Shuffle(len(settle)) {
		if err := c.notify(g, settle[k]); err != nil {
			return err
		}
	}
	if t.Exit {
		g.L.Truth.Exited = true
	}
	return nil
}

// notify reports a background task's end: the queued notification, the
// prompt Claude Code makes of it, a reply and the turn's end.
func (c *Claude) notify(g *Gen, task string) error {
	text := "<task-notification>\n<task-id>" + task + "</task-id>\n<status>completed</status>\n<summary>" + task + " finished</summary>\n</task-notification>"
	// A queued notification carries no uuid and no isSidechain.
	at := g.Tick()
	if err := g.Emit(c.main, at, map[string]any{"type": "queue-operation", "operation": "enqueue", "timestamp": stamp(at),
		"sessionId": c.session, "content": text}); err != nil {
		return err
	}
	n := c.user(text)
	n["origin"] = map[string]any{"kind": "task-notification"}
	if err := c.entry(g, c.main, "", n); err != nil {
		return err
	}
	for i := range g.L.Truth.Agents {
		if g.L.Truth.Agents[i].Task == task {
			g.L.Truth.Agents[i].Settled = true
		}
	}
	if err := c.entry(g, c.main, "", c.assistant(g, map[string]any{"type": "text", "text": task + " is back."})); err != nil {
		return err
	}
	return c.turnEnd(g)
}

// batch starts background Agent calls, one per assistant message, and
// writes each agent's file in a seeded order. It returns the agent ids;
// foreground batches are not written for Claude Code yet.
func (c *Claude) batch(g *Gen, b Batch) ([]string, error) {
	if !b.Background {
		return nil, fmt.Errorf("sessiongen: claude profile writes background batches only")
	}
	ids := make([]string, b.Size)
	prompts := make([]string, b.Size)
	for i := range ids {
		ids[i] = "a" + g.Hex(16)
		prompts[i] = fmt.Sprintf("Agent %s: %s", ids[i][:6], g.Text(40))
		call := "toolu_" + g.Hex(24)
		desc := "Check part " + ids[i][:6]
		if err := c.entry(g, c.main, "", c.assistant(g, toolUse(call, "Agent", map[string]any{"description": desc,
			"subagent_type": "general-purpose", "run_in_background": true, "prompt": prompts[i]}))); err != nil {
			return nil, err
		}
		if err := c.entry(g, c.main, "", c.result(call, "Async agent launched", map[string]any{
			"status": "async_launched", "agentId": ids[i], "description": desc})); err != nil {
			return nil, err
		}
	}
	for _, i := range g.Shuffle(b.Size) {
		path := filepath.Join(strings.TrimSuffix(c.main, ".jsonl"), "subagents", "agent-"+ids[i]+".jsonl")
		g.L.Subagents = append(g.L.Subagents, path)
		g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: ids[i], Prompt: prompts[i], File: path})
		if err := c.entry(g, path, ids[i], c.user(prompts[i])); err != nil {
			return nil, err
		}
		for s := 0; s < b.Steps; s++ {
			id := "toolu_" + g.Hex(24)
			if err := c.entry(g, path, ids[i], c.assistant(g, toolUse(id, "Bash", map[string]any{"command": "echo " + g.Hex(4)}))); err != nil {
				return nil, err
			}
			if err := c.entry(g, path, ids[i], c.result(id, g.Text(30), nil)); err != nil {
				return nil, err
			}
		}
		if err := c.entry(g, path, ids[i], c.assistant(g, map[string]any{"type": "text", "text": "Done: " + g.Text(30)})); err != nil {
			return nil, err
		}
	}
	return ids, nil
}
