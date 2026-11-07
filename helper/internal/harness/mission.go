package harness

import (
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Mission control section kinds, in their fixed display order.
const (
	SectionNeeds      = "needs"
	SectionAgents     = "agents"
	SectionBackground = "background"
	SectionPlan       = "plan"
	SectionChanges    = "changes"
	SectionPorts      = "ports"
	SectionStatus     = "status"
)

// SectionOrder is the order sections appear in, everywhere.
var SectionOrder = []string{
	SectionNeeds, SectionAgents, SectionBackground, SectionPlan,
	SectionChanges, SectionPorts, SectionStatus,
}

// Mission is a session's mission control snapshot. Rev increases with every
// change; a Delta applies to the snapshot whose Rev is Delta.Rev - 1.
type Mission struct {
	Rev     int64   `json:"rev"`
	Summary Summary `json:"summary"`
	// Sections holds only the sections the harness supports, in
	// SectionOrder. An empty section is still listed.
	Sections []Section `json:"sections"`
}

// Summary feeds the activity pill and the roster line (Session.mission).
type Summary = wire.MissionSummary

// Progress is done out of total.
type Progress = wire.Progress

// Section is one mission control section. Status sections carry Status;
// every other kind carries Rows.
type Section struct {
	Kind   string       `json:"kind"`
	Rows   []Row        `json:"rows"`
	Status *StatusBlock `json:"status,omitempty"`
}

// Row states beyond the run states in item.go, for plan rows.
const (
	RowPending = "pending"
	RowActive  = "active"
	RowDone    = "done"
	RowSkipped = "skipped"
)

// Row is a line in a mission control section or a Live list screen.
type Row struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Subtitle string     `json:"subtitle,omitempty"`
	State    string     `json:"state"`
	Started  *time.Time `json:"startedAt,omitempty"`
	// Progress is 0...1, or null when unknown.
	Progress *float64 `json:"progress,omitempty"`
	// Tail is the last lines of live output, at most 4, redacted.
	Tail     []string    `json:"tail,omitempty"`
	Open     *Open       `json:"open,omitempty"`
	Actions  []ActionRef `json:"actions,omitempty"`
	Children []Row       `json:"children,omitempty"`
}

// Open targets.
const (
	OpenChat     = "chat"     // Ref is an agent item id: its conversation
	OpenTerminal = "terminal" // Ref is a session id, or a background job's pane
	OpenDiff     = "diff"     // Ref is a path relative to the session cwd
	OpenURL      = "url"      // Ref is a port number or an http URL
	OpenScreen   = "screen"   // Ref is a screen id for harness.screen
	OpenApproval = "approval" // Ref is a request id
)

// Open says what tapping a row opens.
type Open struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// StatusBlock is the Status primitive: model, mode, context and usage.
type StatusBlock struct {
	Model  string `json:"model,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Effort string `json:"effort,omitempty"`
	// ContextPercent is how full the context window is, 0...100.
	ContextPercent *float64    `json:"contextPercent,omitempty"`
	CostUSD        *float64    `json:"costUsd,omitempty"`
	Meters         []Meter     `json:"meters,omitempty"`
	Facts          []Fact      `json:"facts,omitempty"`
	Actions        []ActionRef `json:"actions,omitempty"`
}

// Meter is a usage gauge with an optional reset time.
type Meter struct {
	ID      string     `json:"id"`
	Label   string     `json:"label"`
	Percent float64    `json:"percent"`
	Resets  *time.Time `json:"resetsAt,omitempty"`
}

// Fact is a label and a value, for status extras and key-value bodies.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Mission delta operations.
const (
	OpUpsert  = "upsert"  // Section, Row: insert or replace the row with Row.ID
	OpRemove  = "remove"  // Section, ID
	OpReplace = "replace" // Section, Rows: replace every row of the section
	OpStatus  = "status"  // Status: replace the status block
	OpSummary = "summary" // Summary: replace the summary
)

// Delta moves a Mission from Rev-1 to Rev. A client whose Rev is not Rev-1
// calls harness.open again.
type Delta struct {
	Rev int64 `json:"rev"`
	Ops []Op  `json:"ops"`
}

// Op is one change to a Mission.
type Op struct {
	Op      string       `json:"op"`
	Section string       `json:"section,omitempty"`
	ID      string       `json:"id,omitempty"`
	Row     *Row         `json:"row,omitempty"`
	Rows    []Row        `json:"rows,omitempty"`
	Status  *StatusBlock `json:"status,omitempty"`
	Summary *Summary     `json:"summary,omitempty"`
}
