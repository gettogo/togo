// Package droid is the Factory Droid harness adapter. Droid's hooks follow
// Claude Code's shape (session_id, transcript_path, permission_mode,
// tool_name, tool_input), so its chat and state come from the shared
// hookstyle reader. Its transcript format has not been seen (it needs a
// login to create one), so no transcript mapper reads it yet.
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
