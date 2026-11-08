// Package crush is the Crush (Charm) adapter. Crush's TUI can be a client
// of a `crush server` that a second client may join, but Togo cannot start
// it that way yet (see the adapter file), so the crush.db mapper reads the
// chat out of the project's .crush/crush.db read-only, sessions are listed
// by `crush session list --json` and changes apply by relaunching.
package crush

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed crush.yaml
var adapterFile []byte

// MapperDB is the mapper name crush.yaml uses.
const MapperDB = "crush.db"

func init() {
	harness.RegisterAdapterFile("crush", adapterFile)
	harness.RegisterMapper(MapperDB, func(env harness.Env) harness.Mapper { return newDB(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
