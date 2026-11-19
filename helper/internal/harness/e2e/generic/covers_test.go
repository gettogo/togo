package generic

import (
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

var (
	cellsOnce sync.Once
	cells     map[string]coverage.Cell
)

// declare says whether a scenario applies to the agent, and declares the
// cell it proves. A cell that does not apply (the adapter declares no such
// capability) is skipped with the reason the inventory gives. One that
// applies runs whether or not it is declared below: a new profile runs
// every scenario before its cells are claimed.
func declare(t *testing.T, agent, cell string) bool {
	t.Helper()
	cellsOnce.Do(func() {
		list, err := coverage.Cells()
		if err != nil {
			t.Fatalf("coverage: %v", err)
		}
		cells = coverage.Index(list)
	})
	key := agent + "/" + cell
	c, ok := cells[key]
	switch {
	case !ok:
		t.Skipf("%s is not a cell of the inventory", key)
	case c.NotApplicable != "":
		t.Skipf("%s does not apply: %s", key, c.NotApplicable)
	}
	if d, ok := declared[key]; ok {
		d(t)
	}
	return true
}

// declared holds, per cell, the literal declaration the coverage scan
// reads (coverage.ScanEvidence takes only string literals). Relaunch is
// claimed only for agents without TestKnownFault_RelaunchKeepsDeadTurnWorking.
// Sending while idle proves part of chat, not the cell.
var declared = map[string]func(t *testing.T){
	"amp/lifecycle/agent_exits":          func(t *testing.T) { coverage.Covers(t, "generated", "amp/lifecycle/agent_exits") },
	"amp/lifecycle/session_removed":      func(t *testing.T) { coverage.Covers(t, "generated", "amp/lifecycle/session_removed") },
	"amp/lifecycle/turn_end":             func(t *testing.T) { coverage.Covers(t, "generated", "amp/lifecycle/turn_end") },
	"amp/lifecycle/connection_drop":      func(t *testing.T) { coverage.Covers(t, "generated", "amp/lifecycle/connection_drop") },
	"amp/lifecycle/interrupt":            func(t *testing.T) { coverage.Covers(t, "generated", "amp/lifecycle/interrupt") },
	"amp/capability/send.queue":          func(t *testing.T) { coverage.Covers(t, "generated", "amp/capability/send.queue") },
	"amp/capability/interrupt":           func(t *testing.T) { coverage.Covers(t, "generated", "amp/capability/interrupt") },
	"amp/capability/needs.approvals":     func(t *testing.T) { coverage.Covers(t, "generated", "amp/capability/needs.approvals") },
	"amp/scale/concurrent_requests":      func(t *testing.T) { coverage.Covers(t, "generated", "amp/scale/concurrent_requests") },
	"claude/lifecycle/agent_exits":       func(t *testing.T) { coverage.Covers(t, "generated", "claude/lifecycle/agent_exits") },
	"claude/lifecycle/session_removed":   func(t *testing.T) { coverage.Covers(t, "generated", "claude/lifecycle/session_removed") },
	"claude/lifecycle/turn_end":          func(t *testing.T) { coverage.Covers(t, "generated", "claude/lifecycle/turn_end") },
	"claude/lifecycle/connection_drop":   func(t *testing.T) { coverage.Covers(t, "generated", "claude/lifecycle/connection_drop") },
	"claude/lifecycle/interrupt":         func(t *testing.T) { coverage.Covers(t, "generated", "claude/lifecycle/interrupt") },
	"claude/capability/send.queue":       func(t *testing.T) { coverage.Covers(t, "generated", "claude/capability/send.queue") },
	"claude/capability/interrupt":        func(t *testing.T) { coverage.Covers(t, "generated", "claude/capability/interrupt") },
	"claude/capability/needs.approvals":  func(t *testing.T) { coverage.Covers(t, "generated", "claude/capability/needs.approvals") },
	"claude/scale/concurrent_requests":   func(t *testing.T) { coverage.Covers(t, "generated", "claude/scale/concurrent_requests") },
	"copilot/lifecycle/agent_exits":      func(t *testing.T) { coverage.Covers(t, "generated", "copilot/lifecycle/agent_exits") },
	"copilot/lifecycle/session_removed":  func(t *testing.T) { coverage.Covers(t, "generated", "copilot/lifecycle/session_removed") },
	"copilot/lifecycle/turn_end":         func(t *testing.T) { coverage.Covers(t, "generated", "copilot/lifecycle/turn_end") },
	"copilot/lifecycle/connection_drop":  func(t *testing.T) { coverage.Covers(t, "generated", "copilot/lifecycle/connection_drop") },
	"copilot/lifecycle/interrupt":        func(t *testing.T) { coverage.Covers(t, "generated", "copilot/lifecycle/interrupt") },
	"copilot/capability/send.queue":      func(t *testing.T) { coverage.Covers(t, "generated", "copilot/capability/send.queue") },
	"copilot/capability/interrupt":       func(t *testing.T) { coverage.Covers(t, "generated", "copilot/capability/interrupt") },
	"copilot/capability/needs.approvals": func(t *testing.T) { coverage.Covers(t, "generated", "copilot/capability/needs.approvals") },
	"copilot/scale/concurrent_requests":  func(t *testing.T) { coverage.Covers(t, "generated", "copilot/scale/concurrent_requests") },
	"cursor/lifecycle/agent_exits":       func(t *testing.T) { coverage.Covers(t, "generated", "cursor/lifecycle/agent_exits") },
	"cursor/lifecycle/session_removed":   func(t *testing.T) { coverage.Covers(t, "generated", "cursor/lifecycle/session_removed") },
	"cursor/lifecycle/turn_end":          func(t *testing.T) { coverage.Covers(t, "generated", "cursor/lifecycle/turn_end") },
	"cursor/lifecycle/connection_drop":   func(t *testing.T) { coverage.Covers(t, "generated", "cursor/lifecycle/connection_drop") },
	"cursor/lifecycle/interrupt":         func(t *testing.T) { coverage.Covers(t, "generated", "cursor/lifecycle/interrupt") },
	"cursor/capability/send.queue":       func(t *testing.T) { coverage.Covers(t, "generated", "cursor/capability/send.queue") },
	"cursor/capability/interrupt":        func(t *testing.T) { coverage.Covers(t, "generated", "cursor/capability/interrupt") },
	"cursor/capability/needs.approvals":  func(t *testing.T) { coverage.Covers(t, "generated", "cursor/capability/needs.approvals") },
	"cursor/scale/concurrent_requests":   func(t *testing.T) { coverage.Covers(t, "generated", "cursor/scale/concurrent_requests") },
	"droid/lifecycle/agent_exits":        func(t *testing.T) { coverage.Covers(t, "generated", "droid/lifecycle/agent_exits") },
	"droid/lifecycle/session_removed":    func(t *testing.T) { coverage.Covers(t, "generated", "droid/lifecycle/session_removed") },
	"droid/lifecycle/turn_end":           func(t *testing.T) { coverage.Covers(t, "generated", "droid/lifecycle/turn_end") },
	"droid/lifecycle/connection_drop":    func(t *testing.T) { coverage.Covers(t, "generated", "droid/lifecycle/connection_drop") },
	"droid/lifecycle/interrupt":          func(t *testing.T) { coverage.Covers(t, "generated", "droid/lifecycle/interrupt") },
	"droid/capability/send.queue":        func(t *testing.T) { coverage.Covers(t, "generated", "droid/capability/send.queue") },
	"droid/capability/interrupt":         func(t *testing.T) { coverage.Covers(t, "generated", "droid/capability/interrupt") },
	"droid/capability/needs.approvals":   func(t *testing.T) { coverage.Covers(t, "generated", "droid/capability/needs.approvals") },
	"droid/scale/concurrent_requests":    func(t *testing.T) { coverage.Covers(t, "generated", "droid/scale/concurrent_requests") },
	"omp/lifecycle/agent_exits":          func(t *testing.T) { coverage.Covers(t, "generated", "omp/lifecycle/agent_exits") },
	"omp/lifecycle/session_removed":      func(t *testing.T) { coverage.Covers(t, "generated", "omp/lifecycle/session_removed") },
	"omp/lifecycle/turn_end":             func(t *testing.T) { coverage.Covers(t, "generated", "omp/lifecycle/turn_end") },
	"omp/lifecycle/connection_drop":      func(t *testing.T) { coverage.Covers(t, "generated", "omp/lifecycle/connection_drop") },
	"omp/lifecycle/interrupt":            func(t *testing.T) { coverage.Covers(t, "generated", "omp/lifecycle/interrupt") },
	"omp/capability/send.queue":          func(t *testing.T) { coverage.Covers(t, "generated", "omp/capability/send.queue") },
	"omp/capability/interrupt":           func(t *testing.T) { coverage.Covers(t, "generated", "omp/capability/interrupt") },
	"omp/scale/concurrent_requests":      func(t *testing.T) { coverage.Covers(t, "generated", "omp/scale/concurrent_requests") },
	"cline/lifecycle/agent_exits":        func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/agent_exits") },
	"cline/lifecycle/relaunch":           func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/relaunch") },
	"cline/lifecycle/session_removed":    func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/session_removed") },
	"cline/lifecycle/turn_end":           func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/turn_end") },
	"cline/lifecycle/connection_drop":    func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/connection_drop") },
	"cline/lifecycle/interrupt":          func(t *testing.T) { coverage.Covers(t, "generated", "cline/lifecycle/interrupt") },
	"cline/capability/send.queue":        func(t *testing.T) { coverage.Covers(t, "generated", "cline/capability/send.queue") },
	"cline/capability/interrupt":         func(t *testing.T) { coverage.Covers(t, "generated", "cline/capability/interrupt") },
	"cline/capability/needs.approvals":   func(t *testing.T) { coverage.Covers(t, "generated", "cline/capability/needs.approvals") },
	"cline/scale/concurrent_requests":    func(t *testing.T) { coverage.Covers(t, "generated", "cline/scale/concurrent_requests") },
	"aider/lifecycle/agent_exits":        func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/agent_exits") },
	"aider/lifecycle/relaunch":           func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/relaunch") },
	"aider/lifecycle/session_removed":    func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/session_removed") },
	"aider/lifecycle/turn_end":           func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/turn_end") },
	"aider/lifecycle/connection_drop":    func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/connection_drop") },
	"aider/lifecycle/interrupt":          func(t *testing.T) { coverage.Covers(t, "generated", "aider/lifecycle/interrupt") },
	"aider/capability/send.queue":        func(t *testing.T) { coverage.Covers(t, "generated", "aider/capability/send.queue") },
	"aider/capability/interrupt":         func(t *testing.T) { coverage.Covers(t, "generated", "aider/capability/interrupt") },
	"aider/scale/concurrent_requests":    func(t *testing.T) { coverage.Covers(t, "generated", "aider/scale/concurrent_requests") },
	"pi/lifecycle/agent_exits":           func(t *testing.T) { coverage.Covers(t, "generated", "pi/lifecycle/agent_exits") },
	"pi/lifecycle/session_removed":       func(t *testing.T) { coverage.Covers(t, "generated", "pi/lifecycle/session_removed") },
	"pi/lifecycle/turn_end":              func(t *testing.T) { coverage.Covers(t, "generated", "pi/lifecycle/turn_end") },
	"pi/lifecycle/connection_drop":       func(t *testing.T) { coverage.Covers(t, "generated", "pi/lifecycle/connection_drop") },
	"pi/lifecycle/interrupt":             func(t *testing.T) { coverage.Covers(t, "generated", "pi/lifecycle/interrupt") },
	"pi/capability/send.queue":           func(t *testing.T) { coverage.Covers(t, "generated", "pi/capability/send.queue") },
	"pi/capability/interrupt":            func(t *testing.T) { coverage.Covers(t, "generated", "pi/capability/interrupt") },
	"pi/scale/concurrent_requests":       func(t *testing.T) { coverage.Covers(t, "generated", "pi/scale/concurrent_requests") },
	"kimi/lifecycle/agent_exits":         func(t *testing.T) { coverage.Covers(t, "generated", "kimi/lifecycle/agent_exits") },
	"kimi/lifecycle/session_removed":     func(t *testing.T) { coverage.Covers(t, "generated", "kimi/lifecycle/session_removed") },
	"kimi/lifecycle/turn_end":            func(t *testing.T) { coverage.Covers(t, "generated", "kimi/lifecycle/turn_end") },
	"kimi/lifecycle/connection_drop":     func(t *testing.T) { coverage.Covers(t, "generated", "kimi/lifecycle/connection_drop") },
	"kimi/lifecycle/interrupt":           func(t *testing.T) { coverage.Covers(t, "generated", "kimi/lifecycle/interrupt") },
	"kimi/capability/send.queue":         func(t *testing.T) { coverage.Covers(t, "generated", "kimi/capability/send.queue") },
	"kimi/capability/interrupt":          func(t *testing.T) { coverage.Covers(t, "generated", "kimi/capability/interrupt") },
	"kimi/capability/needs.approvals":    func(t *testing.T) { coverage.Covers(t, "generated", "kimi/capability/needs.approvals") },
	"kimi/scale/concurrent_requests":     func(t *testing.T) { coverage.Covers(t, "generated", "kimi/scale/concurrent_requests") },
}
