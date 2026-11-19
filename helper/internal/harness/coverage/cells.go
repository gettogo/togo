// Package coverage is the inventory of what the helper's tests must cover,
// and the ratchet that keeps it from growing unnoticed.
//
// The expected cells are computed from the source of truth: every agent in
// internal/agents crossed with what its adapter file under
// internal/harness/adapters declares. Nothing is typed by hand, so a new
// agent or a new capability adds cells and, until tests cover them, adds
// lines the ratchet refuses.
//
// Tests declare what they cover with Covers. The declarations are read
// statically (ScanEvidence), never collected at run time. A cell counts as
// covered when a test of tier "tape" (a real recording replayed through
// the engine) or "generated" (a session generator) names it; "unit" and
// "live" evidence is recorded and shown but does not satisfy a cell.
// missing.txt lists the cells that are not covered yet. It can only
// shrink.
package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Cell kinds.
const (
	KindCapability = "capability"
	KindLifecycle  = "lifecycle"
	KindScale      = "scale"
)

// Lifecycle events every adapter is asked about.
var Lifecycle = []string{
	"agent_exits", "relaunch", "new_conversation", "resume", "fork",
	"session_removed", "helper_restart", "connection_drop", "turn_end", "interrupt",
}

// Scale cases asked of every adapter with native chat.
var Scale = []string{"long_session", "at_limit", "concurrent_requests"}

// Cell is one thing a test must cover for one agent.
type Cell struct {
	Agent string
	Kind  string
	Name  string
	// NotApplicable says why the agent cannot have this cell, computed from
	// its adapter file; empty when the cell applies.
	NotApplicable string
}

// Key is the stable name of a cell: agent/kind/name.
func (c Cell) Key() string { return c.Agent + "/" + c.Kind + "/" + c.Name }

// HelperDir is the helper module directory, found from this source file.
// It is empty when the source tree is not on disk (a trimmed build).
func HelperDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..")
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return ""
	}
	return filepath.Clean(dir)
}

// AdapterDir is the directory holding one folder per adapter.
func AdapterDir() string {
	if h := HelperDir(); h != "" {
		return filepath.Join(h, "internal", "harness", "adapters")
	}
	return ""
}

// Cells returns the expected cells of the real tree, sorted by key.
func Cells() ([]Cell, error) {
	dir := AdapterDir()
	if dir == "" {
		return nil, fmt.Errorf("coverage: the adapter directory is not on disk")
	}
	return CellsFrom(dir, agents.Registry)
}

// adapterInfo is what the cells need from the adapter files of one agent.
type adapterInfo struct {
	files []string
	caps  map[string]bool
	fork  bool
}

// CellsFrom derives the cells from the adapter files in dir/*/*.yaml and
// the given agents. An agent with several files (an adapter per major
// version) gets the union of their capabilities.
func CellsFrom(dir string, registry []agents.Agent) ([]Cell, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	known := map[string]bool{}
	for _, a := range registry {
		known[a.ID] = true
	}
	infos := map[string]*adapterInfo{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		a, err := spec.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("coverage: %s: %w", p, err)
		}
		rel, _ := filepath.Rel(dir, p)
		for _, id := range a.Match.Agents {
			if !known[id] {
				return nil, fmt.Errorf("coverage: %s matches agent %q, which is not in the agent registry", rel, id)
			}
			in := infos[id]
			if in == nil {
				in = &adapterInfo{caps: map[string]bool{}}
				infos[id] = in
			}
			in.files = append(in.files, filepath.ToSlash(rel))
			for _, c := range a.Capabilities {
				in.caps[c] = true
			}
			in.fork = in.fork || declaresFork(a)
		}
	}
	var cells []Cell
	for _, ag := range registry {
		cells = append(cells, agentCells(ag, infos[ag.ID])...)
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].Key() < cells[j].Key() })
	for i := 1; i < len(cells); i++ {
		if cells[i].Key() == cells[i-1].Key() {
			return nil, fmt.Errorf("coverage: duplicate cell %s", cells[i].Key())
		}
	}
	return cells, nil
}

func declaresFork(a *spec.Adapter) bool {
	if a.Commands == nil {
		return false
	}
	for _, c := range a.Commands.Static {
		if c.Name == "fork" {
			return true
		}
	}
	_, ok := a.Commands.Intents["fork"]
	return ok
}

func agentCells(ag agents.Agent, in *adapterInfo) []Cell {
	var cells []Cell
	add := func(kind, name, na string) {
		cells = append(cells, Cell{Agent: ag.ID, Kind: kind, Name: name, NotApplicable: na})
	}
	if in != nil {
		caps := make([]string, 0, len(in.caps))
		for c := range in.caps {
			caps = append(caps, c)
		}
		sort.Strings(caps)
		for _, c := range caps {
			add(KindCapability, c, "")
		}
	}
	files := "no adapter file"
	if in != nil {
		files = strings.Join(in.files, ", ")
	}
	need := func(capability string) string {
		if in == nil {
			return "no adapter file"
		}
		if !in.caps[capability] {
			return fmt.Sprintf("%s declares no %q capability", files, capability)
		}
		return ""
	}
	for _, ev := range Lifecycle {
		na := ""
		switch ev {
		case "agent_exits", "relaunch":
			if in == nil {
				na = "no adapter file"
			} else if ag.Command == "" {
				na = "the agent registry gives " + ag.ID + " no command to exit or relaunch"
			}
		case "new_conversation", "turn_end":
			na = need("chat")
		case "resume":
			na = need("resume")
		case "interrupt":
			na = need("interrupt")
		case "fork":
			if in == nil {
				na = "no adapter file"
			} else if !in.fork {
				na = files + " declares no fork command"
			}
		default: // session_removed, helper_restart, connection_drop
			if in == nil {
				na = "no adapter file"
			}
		}
		add(KindLifecycle, ev, na)
	}
	for _, sc := range Scale {
		add(KindScale, sc, need("chat"))
	}
	return cells
}

// Index maps keys to cells.
func Index(cells []Cell) map[string]Cell {
	m := make(map[string]Cell, len(cells))
	for _, c := range cells {
		m[c.Key()] = c
	}
	return m
}

// Agents lists the agent ids of cells in first-seen order.
func Agents(cells []Cell) []string {
	var out []string
	for _, c := range cells {
		if !slices.Contains(out, c.Agent) {
			out = append(out, c.Agent)
		}
	}
	return out
}
