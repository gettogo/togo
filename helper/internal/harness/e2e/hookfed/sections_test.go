package hookfed

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The Changes and Ports sections of the hook-driven agents. The sections
// are the engine's (support/sections.go); what is each agent's is that its
// adapter turns them on and that the end of its turn, which its Stop hook
// reports, refreshes Changes.

// secondPrompt is the index of the call that opens the second turn.
func secondPrompt(t *testing.T, a hookAgent, calls []sessiongen.HookCall) int {
	t.Helper()
	n := 0
	for i, c := range calls {
		for _, k := range a.classify(c) {
			if k.what == "prompt" {
				if n++; n == 2 {
					return i
				}
			}
		}
	}
	t.Fatal("the session has no second prompt")
	return 0
}

func twoTurns() []sessiongen.Turn {
	return []sessiongen.Turn{{Say: 30, Tools: 1, Output: 20}, {Say: 30, Tools: 1, Output: 20}}
}

// changesFollowTheTree: the first turn is read, the tree changes, and the
// section follows it once the second turn's Stop hook ends that turn.
func changesFollowTheTree(t *testing.T, a hookAgent) {
	t.Helper()
	e := support.NewEnv(t, time.Hour)
	support.GitTree(t, e.Cwd)
	l := write(t, e, a, twoTurns())
	cut := secondPrompt(t, a, l.Hooks)
	r := e.RigWith(t, enginetest.Options{Agent: a.name, NoRedact: true, Git: true})
	run(t, r, a, l.Hooks[:cut])
	settled(r, lastPrompt(a, l.Hooks[:cut]))
	support.ChangesScenario(t, r, e.Cwd, func() func(enginetest.State) bool {
		run(t, r, a, l.Hooks[cut:])
		return support.TurnDone(lastPrompt(a, l.Hooks))
	})
}

// portsAreTheListeners: the listeners show beside the chat, follow a
// server that stops, and survive the helper's restart.
func portsAreTheListeners(t *testing.T, a hookAgent) {
	t.Helper()
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, twoTurns()[:1])
	r := e.RigWith(t, enginetest.Options{Agent: a.name, NoRedact: true, Ports: true, Listeners: support.Dev})
	run(t, r, a, l.Hooks)
	settled(r, lastPrompt(a, l.Hooks))
	support.PortsScenario(t, r, nil, support.TurnDone(lastPrompt(a, l.Hooks)))
}

func TestCopilotChangesFollowTheTree(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/capability/changes")
	changesFollowTheTree(t, copilotAgent)
}

func TestCopilotPortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "copilot/capability/ports")
	portsAreTheListeners(t, copilotAgent)
}

func TestDroidChangesFollowTheTree(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "droid/capability/changes")
	changesFollowTheTree(t, droidAgent)
}

func TestDroidPortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "droid/capability/ports")
	portsAreTheListeners(t, droidAgent)
}
