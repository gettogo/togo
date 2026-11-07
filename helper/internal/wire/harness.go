package wire

// MissionSummary is Session.mission (harness.v1): the activity pill and the
// roster line for a session whose agent has a harness adapter. Absent for
// sessions with no adapter.
type MissionSummary struct {
	NeedsYou   int `json:"needsYou"`
	Agents     int `json:"agents"`
	Background int `json:"background"`
	// Plan is null when the harness has no plan or the plan is empty.
	Plan *Progress `json:"plan"`
	// Working is true while a turn runs.
	Working bool `json:"working"`
}

// Progress is done out of total.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
