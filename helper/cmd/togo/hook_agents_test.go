package main

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/local"
)

// Every agent `togo hook` forwards must be one the daemon's local.hook
// accepts; otherwise its hooks fire, the daemon answers "unknown agent"
// and the chat says the hooks never fired (seen live with Cursor).
func TestEveryHookAgentReachesTheDaemon(t *testing.T) {
	for agent := range forwardAdapters {
		if !local.HookAgents[agent] {
			t.Errorf("togo hook %s forwards events that local.hook rejects as an unknown agent", agent)
		}
	}
	for agent := range hookAdapters {
		if !local.HookAgents[agent] {
			t.Errorf("togo hook %s reports events that local.hook rejects as an unknown agent", agent)
		}
	}
}
