// Package cursor is the Cursor CLI harness adapter. Cursor's hooks
// (~/.cursor/hooks.json) use snake_case keys under its own event names, so
// its chat and state come from the shared hookstyle reader with the cursor
// dialect; `togo hook cursor <event>` forwards them once installed.
package cursor

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

//go:embed cursor.yaml
var adapterFile []byte

// MapperHooks is the hooks mapper name cursor.yaml uses.
const MapperHooks = "cursor.hooks"

func init() {
	harness.RegisterAdapterFile("cursor", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks})
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
