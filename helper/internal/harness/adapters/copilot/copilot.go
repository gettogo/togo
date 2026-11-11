// Package copilot is the GitHub Copilot CLI harness adapter. Copilot's
// hooks are a camelCase variant of Claude Code's (toolName, toolArgs as
// JSON text, toolResult); hookstyle normalizes them, so its chat and state
// come from the shared reader. Copilot also runs the hooks in
// .claude/settings*.json; local.Foreign keeps Claude's copies out of a
// Copilot session. The per-session event log
// (~/.copilot/session-state/<id>/events.jsonl) is undocumented and was not
// seen live (it needs a login), so only the session list reads it.
package copilot

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

//go:embed copilot.yaml
var adapterFile []byte

// MapperHooks is the hooks mapper name copilot.yaml uses.
const MapperHooks = "copilot.hooks"

func init() {
	harness.RegisterAdapterFile("copilot", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks, Replies: replies, Ask: askQuestion, Answered: askAnswered, Plan: planText})
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
