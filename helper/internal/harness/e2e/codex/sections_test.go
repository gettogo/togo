package codex

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The Ports section is the engine's (support/sections.go): the session's
// listeners, each with its port and program, opening as a URL. For Codex
// they are listed beside the chat the app-server gives, and stay after the
// helper restarts and Codex's server is joined again.
func TestCodexPortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/capability/ports")
	c := codexStart(t, 91, func(o *enginetest.Options) { o.Ports, o.Listeners = true, support.Dev },
		sessiongen.Turn{Say: 20, Tools: 1, Output: 20})
	support.SettleUntil(c.r, 1)
	support.PortsScenario(t, c.r, nil, support.HasPrompt(1))
}
