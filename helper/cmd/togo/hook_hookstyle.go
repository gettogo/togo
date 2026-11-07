package main

import (
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/wire"
)

// forwardAdapter maps one agent's hook call to local.hook params that carry
// the redacted payload, and says when and how its hook answers an
// approval.
type forwardAdapter struct {
	reduce   func(event string, payload []byte, env map[string]string) (wire.HookForward, error)
	blocks   func(event string) bool
	decision func(answer string) []byte
}

// forwardAdapters holds the agents whose hooks follow Claude Code's shape
// (droid, vibe, copilot).
var forwardAdapters = map[string]forwardAdapter{}

func init() {
	for _, agent := range hookstyle.Agents() {
		d, _ := hookstyle.For(agent)
		forwardAdapters[agent] = forwardAdapter{
			reduce: func(event string, payload []byte, env map[string]string) (wire.HookForward, error) {
				return hookstyle.Reduce(d, event, payload, env)
			},
			blocks:   func(event string) bool { return hookstyle.Blocks(d, d.Canonical(event)) },
			decision: func(answer string) []byte { return hookstyle.Decision(d, answer) },
		}
	}
}
