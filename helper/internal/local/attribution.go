package local

import (
	"context"
	"sync"

	"github.com/gettogo/togo/helper/internal/agents"
)

// HookAgents are the agents whose `togo hook <agent>` reports reach
// local.hook.
var HookAgents = map[string]bool{
	"claude": true, "codex": true, "droid": true, "vibe": true, "copilot": true,
	"auggie": true,
}

// Foreign reports whether a hook for agent fired inside a session started
// for another agent. Harnesses run each other's hook files (Copilot CLI
// runs the hooks in .claude/settings*.json), so `togo hook claude` also
// fires in a Copilot session; its events describe Copilot's turn in
// Claude's words and would be read as the wrong harness. A session started
// for the plain shell, or whose agent is unknown, accepts any agent: the
// user may run any harness in it.
func Foreign(owner, agent string) bool {
	if owner == "" || owner == "shell" || owner == agent {
		return false
	}
	_, known := agents.Lookup(owner)
	return known
}

// AgentCache remembers each session's agent, which never changes after
// the session starts, so a hook costs one lookup per session.
type AgentCache struct {
	// Lookup returns a session's agent id, or "" when the session is not
	// found.
	Lookup func(ctx context.Context, sessionID string) string

	mu sync.Mutex
	m  map[string]string
}

const maxAgentCache = 1024

// Agent returns the session's agent id, or "" when unknown.
func (c *AgentCache) Agent(ctx context.Context, id string) string {
	c.mu.Lock()
	a, ok := c.m[id]
	c.mu.Unlock()
	if ok || c.Lookup == nil {
		return a
	}
	a = c.Lookup(ctx, id)
	if a == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= maxAgentCache {
		c.m = map[string]string{}
	}
	c.m[id] = a
	return a
}

// agentTitle is the agent's display name for "<agent> finished".
func agentTitle(agent string) string {
	if agent == "claude" {
		return "Claude"
	}
	if a, ok := agents.Lookup(agent); ok && a.Name != "" {
		return a.Name
	}
	return "Claude"
}
