package apifed

import (
	"strconv"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The Ports and Changes sections (support/sections.go) of the agents the
// helper drives through an API.

// apiPortsAreTheListeners: OpenCode 1.x and Kilo Code run their server
// in the pane's process tree, so the Ports section lists it, named for the
// agent, beside what the agent started.
func apiPortsAreTheListeners(t *testing.T, agent string) {
	t.Helper()
	s := apiStart(t, agent, 95, func(o *enginetest.Options) { o.Ports, o.Listeners = true, support.Dev }, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	port := s.api.Port()
	if port == 0 {
		t.Fatal("the agent's server has no port")
	}
	// A restart shows no chat until the agent's next event
	// (TestKnownFault_OpenCode_RestartLosesChat): the ports are what is asked.
	support.PortsScenario(t, s.r, map[string]string{strconv.Itoa(port): agent}, nil)
}

func TestOpenCodePortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/ports")
	apiPortsAreTheListeners(t, "opencode")
}

func TestKiloPortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/ports")
	apiPortsAreTheListeners(t, "kilo")
}

func TestOpenCode2PortsAreTheListeners(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/ports")
	s := oc2Start(t, 96, func(o *enginetest.Options) { o.Ports, o.Listeners = true, support.Dev }, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	// The chat is not asked for after the restarts: beside other 2.x tests
	// it sometimes takes 30 seconds to come back, which is not what this
	// test is about.
	support.PortsScenario(t, s.r, nil, nil)
}

// OpenCode 2.x has no diff event (sessiongen/opencode2.go): the Changes
// section is git's view of the folder, refreshed when a turn ends.
func TestOpenCode2ChangesFollowTheTree(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/changes")
	e := support.NewEnv(t, 10*time.Minute)
	support.GitTree(t, e.Cwd)
	s := oc2StartIn(t, e, 97, func(o *enginetest.Options) { o.Git = true }, sessiongen.Turn{Say: 20, Tools: 1, Output: 20})
	support.SettleUntil(s.r, 1)
	support.ChangesScenario(t, s.r, s.e.Cwd, func() func(enginetest.State) bool {
		s.add(t, sessiongen.Turn{Say: 20})
		return func(st enginetest.State) bool { return support.HasPrompt(2)(st) && !st.Mission.Summary.Working }
	})
}

// apiMCP: the MCP servers the agent's server lists are the rows of the
// MCP screen, each with the state the server gave. 1.x has no connect or
// disconnect route, so no row offers an action.
func apiMCP(t *testing.T, agent string) {
	t.Helper()
	s := apiStart(t, agent, 98, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(s.r, 1)
	sc, err := s.r.Screen("mcp", nil)
	if err != nil || sc.LiveList == nil {
		t.Fatalf("mcp screen: %+v %v", sc, err)
	}
	got := map[string]string{}
	for _, r := range sc.LiveList.Rows {
		got[r.ID] = r.Title + "|" + r.State
		if len(r.Actions) != 0 {
			t.Errorf("row %s offers %+v: %s has no route to connect or disconnect", r.ID, r.Actions, agent)
		}
	}
	want := map[string]string{"broken_tools": "broken_tools|failed", "fixture_tools": "fixture_tools|connected", "off_tools": "off_tools|disabled"}
	if len(got) != len(want) {
		t.Fatalf("mcp rows %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("mcp row %s: %q, want %q", id, got[id], w)
		}
	}
}

func TestOpenCodeMCPServers(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode/capability/mcp")
	apiMCP(t, "opencode")
}

func TestKiloMCPServers(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "kilo/capability/mcp")
	apiMCP(t, "kilo")
}
