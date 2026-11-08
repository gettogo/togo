// Package droid is the Factory Droid harness adapter. Droid's hooks follow
// Claude Code's shape (session_id, transcript_path, permission_mode,
// tool_name, tool_input), so its chat and state come from the shared
// hookstyle reader. No transcript mapper draws the chat yet; a Stop hook
// reads only the transcript's last agent_turn_outcome, which says whether
// the turn failed and why (hookstyle/failures.go, seen on 1.5.0).
package droid

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

//go:embed droid.yaml
var adapterFile []byte

// MapperHooks is the hooks mapper name droid.yaml uses.
const MapperHooks = "droid.hooks"

func init() {
	harness.RegisterAdapterFile("droid", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks})
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
