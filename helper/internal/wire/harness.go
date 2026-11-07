package wire

import "encoding/json"

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

// MaxHookPayload bounds the raw hook payload local.hook forwards.
const MaxHookPayload = 256 << 10

// HookStatusline is HookForward.Hook for a status line payload.
const HookStatusline = "statusline"

// HookForward is the local.hook params with the agent's raw hook payload
// (harness.v1), for the harness adapter's hook and status line feeds. Hook
// is the agent's own event name (PreToolUse, or statusline); Payload is
// redacted in the hook process and at most MaxHookPayload bytes. Helpers
// that predate it send a plain HookEvent, which decodes with both empty.
type HookForward struct {
	HookEvent
	Hook    string          `json:"hook,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
