package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	harness.RegisterAdapterFile("fakeexit", []byte(`
adapter: fakeexit
name: Fake exit
revision: 1
match: { agents: [fakeexit] }
capabilities: [chat, agents, background]
feeds:
  events: { hook: { events: [Event] }, mapper: fake.events, stability: documented }
`))
}

// A subagent or a background job runs inside the agent's process: when the
// process exits nothing reports their end, and their rows would say running
// for as long as the session is kept.
func TestSubagentsAndJobsEndWhenTheAgentExits(t *testing.T) {
	id := "s_exitrows01"
	var status atomic.Value
	status.Store("working")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status, func(c *Config) {
		c.Sessions = fakeSessions{id: {ID: id, Agent: "fakeexit", Managed: true, Cwd: c.Home, Command: []string{"fakeexit"}}}
	})
	s, _ := e.served(context.Background(), id)
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Second)
	deliver(id, map[string]any{
		"items": []harness.Item{
			{ID: "agent-a", Kind: harness.KindAgent, Agent: &harness.AgentBody{AgentID: "a", Name: "Audit", State: harness.StateRunning, Started: started}},
			{ID: "agent-b", Kind: harness.KindAgent, Agent: &harness.AgentBody{AgentID: "b", Name: "Done", State: harness.StateOK, Started: started, Ended: &started}},
			{ID: "job-1", Kind: harness.KindBackground, Background: &harness.BackgroundBody{JobID: "1", Kind: harness.BackgroundShell, Title: "Server", State: harness.StateRunning, Started: started}},
		},
		"ops": []harness.Op{
			{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &harness.Row{ID: "agent-a", Title: "Audit", State: harness.StateRunning}},
			{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &harness.Row{ID: "agent-b", Title: "Done", State: harness.StateOK}},
			{Op: harness.OpUpsert, Section: harness.SectionBackground, Row: &harness.Row{ID: "job-1", Title: "Server", State: harness.StateRunning}},
		}})
	waitFor(t, func() bool { sum := s.sum.Load(); return sum.Agents == 1 && sum.Background == 1 })

	status.Store(wire.StatusEnded)
	sid := id
	e.Observe(wire.Event{Kind: wire.EventSession, SessionID: &sid, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Agent: "fakeexit", Managed: true, Status: wire.StatusEnded},
	}})
	waitFor(t, func() bool { sum := s.sum.Load(); return sum.Agents == 0 && sum.Background == 0 })
	waitFor(t, func() bool {
		a, ok := e.log.Get(id, "agent-a")
		j, ok2 := e.log.Get(id, "job-1")
		return ok && ok2 && a.Agent.State == harness.StateInterrupted && a.Agent.Ended != nil &&
			j.Background.State == harness.StateInterrupted && j.Background.Ended != nil
	})
	if b, _ := e.log.Get(id, "agent-b"); b.Agent.State != harness.StateOK {
		t.Errorf("a finished agent was changed: %+v", b.Agent)
	}
	rows := map[string]string{}
	s.mu.Lock()
	snap := s.mission.snapshot()
	s.mu.Unlock()
	for _, sec := range snap.Sections {
		for _, r := range sec.Rows {
			rows[r.ID] = r.State
		}
	}
	if rows["agent-a"] != harness.StateInterrupted || rows["job-1"] != harness.StateInterrupted || rows["agent-b"] != harness.StateOK {
		t.Errorf("rows after the exit: %v", rows)
	}
}

// AgentsRunning is the pane watcher's question before it calls a silent
// pane a finished turn: yes while an agent row runs, no for a background
// job alone (a dev server lasts as long as the session), no once the agent
// has finished or the session's own agent is gone.
func TestAgentsRunningCountsAgentsNotJobs(t *testing.T) {
	id := "s_agentsrun1"
	var status atomic.Value
	status.Store("working")
	e := statusEngine(t, id, &seqPane{mode: "a"}, &status, func(c *Config) {
		c.Sessions = fakeSessions{id: {ID: id, Agent: "fakeexit", Managed: true, Cwd: c.Home, Command: []string{"fakeexit"}}}
	})
	s, _ := e.served(context.Background(), id)
	sub := &fakeSub{done: make(chan struct{})}
	defer close(sub.done)
	if _, err := e.Open(context.Background(), sub, id, 0, 50); err != nil {
		t.Fatal(err)
	}
	if e.AgentsRunning(id) || e.AgentsRunning("s_unknown") {
		t.Fatal("agents running before any started")
	}
	row := func(section, rowID, state string) harness.Op {
		return harness.Op{Op: harness.OpUpsert, Section: section, Row: &harness.Row{ID: rowID, Title: rowID, State: state}}
	}
	deliver(id, map[string]any{"ops": []harness.Op{row(harness.SectionBackground, "job-1", harness.StateRunning)}})
	waitFor(t, func() bool { return s.sum.Load().Background == 1 })
	if e.AgentsRunning(id) {
		t.Fatal("a background job alone counted as an agent running")
	}
	deliver(id, map[string]any{"ops": []harness.Op{row(harness.SectionAgents, "agent-a", harness.StateRunning)}})
	waitFor(t, func() bool { return e.AgentsRunning(id) })
	deliver(id, map[string]any{"ops": []harness.Op{row(harness.SectionAgents, "agent-a", harness.StateOK)}})
	waitFor(t, func() bool { return !e.AgentsRunning(id) })
	deliver(id, map[string]any{"ops": []harness.Op{row(harness.SectionAgents, "agent-b", harness.StateRunning)}})
	waitFor(t, func() bool { return e.AgentsRunning(id) })
	status.Store(wire.StatusEnded)
	if e.AgentsRunning(id) {
		t.Fatal("agents running after the session's agent ended")
	}
}
