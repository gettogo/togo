package filefed

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The Changes and Ports sections for the agents that write their chat to
// files or a database. The sections are the engine's (support/sections.go);
// what is each agent's is that its adapter turns them on and that the end
// of its own turn refreshes Changes.

// withSections is the agent with a rig that has git and a listing of the
// session's listening ports.
func withSections(a fileAgent) fileAgent {
	a.opts = func(o *enginetest.Options) {
		o.Git, o.Ports, o.Listeners = true, true, support.Dev
	}
	return a
}

// changesFollowTheTree runs the Changes scenario over a file agent: its
// next turn ends under the engine, and the section follows the tree.
func changesFollowTheTree(t *testing.T, a fileAgent, seed int64) {
	t.Helper()
	a = withSections(a)
	e := support.NewEnv(t, time.Hour)
	support.GitTree(t, e.Cwd)
	l, r := a.start(t, e, sessiongen.Script{Seed: seed})
	a.attach(t, r, l)
	support.ChangesScenario(t, r, e.Cwd, func() func(enginetest.State) bool {
		support.Now(l)
		a.add(t, l, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
		return reached(l, false)
	})
}

// portsAreTheListeners runs the Ports scenario over a file agent; chat
// says whether the chat must be back after each restart.
func portsAreTheListeners(t *testing.T, a fileAgent, seed int64, chat bool) {
	t.Helper()
	a = withSections(a)
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: seed})
	a.attach(t, r, l)
	var then func(enginetest.State) bool
	if chat {
		then = enginetest.Has(l.Truth.Last)
	}
	support.PortsScenario(t, r, nil, then)
}

func TestGooseChangesFollowTheTree(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/capability/changes")
	changesFollowTheTree(t, gooseAgent, 81)
}

func TestGoosePortsAreTheListeners(t *testing.T) {
	needSQLite(t)
	coverage.Covers(t, "generated", "goose/capability/ports")
	portsAreTheListeners(t, gooseAgent, 82, true)
}

func TestVibeChangesFollowTheTree(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/capability/changes")
	changesFollowTheTree(t, vibeAgent, 83)
}

// A restart that falls between two turns shows no chat for Vibe
// (TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing), so the chat is
// not asked for here; the ports are.
func TestVibePortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "vibe/capability/ports")
	portsAreTheListeners(t, vibeAgent, 84, false)
}

// sectionRig writes the first turns of a generated session and starts a
// rig with git and listening ports over them.
func sectionRig(t *testing.T, e support.Env, agent string, p sessiongen.Profile, seed int64, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	l, err := sessiongen.Write(e.Home, p, sessiongen.Script{Seed: seed, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	r := e.RigWith(t, enginetest.Options{Agent: agent, NoRedact: true, Git: true, Ports: true, Listeners: support.Dev})
	return l, r
}

func TestAiderPortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "aider/capability/ports")
	_, r := sectionRig(t, support.NewEnv(t, 10*time.Minute), "aider", &sessiongen.Aider{}, 85, sessiongen.Turn{Say: 20})
	support.SettleUntil(r, 1)
	support.PortsScenario(t, r, nil, support.HasPrompt(1))
}

// Cline's file has no turn state: the pane going idle ends the turn, and
// that is what refreshes the Changes section.
func TestClineChangesFollowTheTree(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/capability/changes")
	e := support.NewEnv(t, 10*time.Minute)
	support.GitTree(t, e.Cwd)
	l, r := sectionRig(t, e, "cline", &sessiongen.Cline{}, 86, sessiongen.Turn{Say: 20, Tools: 1, Output: 20})
	r.Hub("working")
	support.SettleUntil(r, 1)
	support.ChangesScenario(t, r, e.Cwd, func() func(enginetest.State) bool {
		support.Now(l)
		if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
			t.Fatal(err)
		}
		support.SettleUntil(r, 2)
		r.Hub("idle")
		return func(st enginetest.State) bool { return !st.Mission.Summary.Working }
	})
}

func TestClinePortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "cline/capability/ports")
	_, r := sectionRig(t, support.NewEnv(t, 10*time.Minute), "cline", &sessiongen.Cline{}, 87, sessiongen.Turn{Say: 20})
	support.SettleUntil(r, 1)
	support.PortsScenario(t, r, nil, support.HasPrompt(1))
}

func TestKimiChangesFollowTheTree(t *testing.T) {
	coverage.Covers(t, "generated", "kimi/capability/changes")
	e := kimiEnv(t, 10*time.Minute)
	support.GitTree(t, e.Cwd)
	l, r := sectionRig(t, e, "kimi", &sessiongen.Kimi{}, 88, sessiongen.Turn{Say: 20, Tools: 1, Output: 20})
	support.SettleUntil(r, 1)
	support.ChangesScenario(t, r, e.Cwd, func() func(enginetest.State) bool {
		support.Now(l)
		if err := l.Append(sessiongen.Turn{Say: 20}); err != nil {
			t.Fatal(err)
		}
		return turnsDone(2)
	})
}

// turnsDone is true once the n-th prompt is shown and no turn runs.
func turnsDone(n int) func(enginetest.State) bool {
	return func(st enginetest.State) bool {
		if !support.HasPrompt(n)(st) {
			return false
		}
		for _, it := range latestItems(st) {
			if it.Turn != nil && support.Running(it.Turn.State) {
				return false
			}
		}
		return true
	}
}
