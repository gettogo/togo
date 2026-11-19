// Package coverage is the inventory of what the helper's tests must cover,
// and the ratchet that keeps it from growing unnoticed.
//
// The expected cells are computed from the source of truth: every agent in
// internal/agents whose adapter file under internal/harness/adapters
// declares the chat capability, crossed with what that adapter declares.
// Nothing is typed by hand, so a new agent or a new capability adds cells
// and, until tests cover them, adds lines the ratchet refuses.
//
// An agent whose adapter declares no chat (Auggie, Kiro CLI, OpenHands, the
// shell) is not a supported agent: the terminal view only names it. One
// row, TerminalAgent, stands for all of them: the lifecycle events that
// apply to a session with no native chat, and the prompts the screen
// watcher reads off the pane.
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

// TerminalAgent is the key of the one row that stands for any other
// command-line agent in the terminal view.
const TerminalAgent = "terminal"

// ScreenPrompt is the terminal row's capability cell: a numbered menu or a
// yes/no prompt on the screen is surfaced to the phone and answered.
const ScreenPrompt = "screen_prompt"

// terminalLifecycle are the lifecycle events that apply to a session with
// no native chat.
var terminalLifecycle = []string{"agent_exits", "relaunch", "session_removed", "helper_restart", "connection_drop"}

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
// the given agents.
//
// An agent with several files (an adapter per major version, as OpenCode
// 1.x and 2.x) gets a row per file, keyed by the file's adapter name: the
// versions declare different capabilities, reach the agent differently
// (OpenCode 1.x takes prompts over its API, 2.x has them typed) and fail
// differently, so a test of one version must not cover a cell of the
// other. An agent with one file keeps the agent's id as its row.
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
	type parsed struct {
		rel string
		a   *spec.Adapter
	}
	var specs []parsed
	files := map[string]int{} // agent id -> adapter files matching it
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
			files[id]++
		}
		specs = append(specs, parsed{filepath.ToSlash(rel), a})
	}
	infos := map[string]*adapterInfo{}
	rows := map[string][]string{} // agent id -> its row names, in file order
	for _, s := range specs {
		for _, id := range s.a.Match.Agents {
			row := id
			if files[id] > 1 {
				row = s.a.Adapter
			}
			in := infos[row]
			if in == nil {
				in = &adapterInfo{caps: map[string]bool{}}
				infos[row] = in
				rows[id] = append(rows[id], row)
			}
			in.files = append(in.files, s.rel)
			for _, c := range s.a.Capabilities {
				in.caps[c] = true
			}
			in.fork = in.fork || declaresFork(s.a)
		}
	}
	var cells []Cell
	for _, ag := range registry {
		for _, row := range rows[ag.ID] {
			if in := infos[row]; in.caps["chat"] {
				cells = append(cells, agentCells(row, ag, in)...)
			}
		}
	}
	cells = append(cells, terminalCells()...)
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

// agentCells are the cells of one row: the agent's, or one version's of
// it when the agent has an adapter file per version.
func agentCells(row string, ag agents.Agent, in *adapterInfo) []Cell {
	var cells []Cell
	add := func(kind, name, na string) {
		cells = append(cells, Cell{Agent: row, Kind: kind, Name: name, NotApplicable: na})
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

// terminalCells is the terminal row: the events a session with no native
// chat goes through, and the screen prompt.
func terminalCells() []Cell {
	const noChat = "the terminal row has no native chat"
	var cells []Cell
	add := func(kind, name, na string) {
		cells = append(cells, Cell{Agent: TerminalAgent, Kind: kind, Name: name, NotApplicable: na})
	}
	add(KindCapability, ScreenPrompt, "")
	for _, ev := range Lifecycle {
		na := noChat
		if slices.Contains(terminalLifecycle, ev) {
			na = ""
		}
		add(KindLifecycle, ev, na)
	}
	for _, sc := range Scale {
		add(KindScale, sc, noChat)
	}
	return cells
}

// TerminalAgents lists the registry agents the terminal row stands for:
// those whose adapter declares no chat capability (or has no adapter file).
func TerminalAgents() ([]string, error) {
	dir := AdapterDir()
	if dir == "" {
		return nil, fmt.Errorf("coverage: the adapter directory is not on disk")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*.yaml"))
	if err != nil {
		return nil, err
	}
	chat := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		a, err := spec.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("coverage: %s: %w", p, err)
		}
		if slices.Contains(a.Capabilities, "chat") {
			for _, id := range a.Match.Agents {
				chat[id] = true
			}
		}
	}
	var out []string
	for _, ag := range agents.Registry {
		if !chat[ag.ID] {
			out = append(out, ag.ID)
		}
	}
	return out, nil
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
