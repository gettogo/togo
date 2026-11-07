package session

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestStoredProgramMatches(t *testing.T) {
	cases := []struct {
		agent, stored, resolved string
		want                    bool
	}{
		{"claude", "claude", "claude", true},
		{"claude", "claude", "/Users/me/.local/bin/claude", true},
		{"claude", "/opt/homebrew/bin/claude", "claude", true},
		{"claude", "claude", "/usr/local/bin/codex", false},
		{wire.AgentCustom, "mytool", "/usr/local/bin/mytool", false},
		{wire.AgentCustom, "/usr/local/bin/mytool", "/usr/local/bin/mytool", true},
	}
	for _, c := range cases {
		if got := storedProgramMatches(c.agent, c.stored, c.resolved); got != c.want {
			t.Errorf("storedProgramMatches(%q, %q, %q) = %v, want %v", c.agent, c.stored, c.resolved, got, c.want)
		}
	}
}
