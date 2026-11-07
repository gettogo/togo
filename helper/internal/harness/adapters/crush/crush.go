// Package crush is the Crush (Charm) adapter. Crush's TUI can be a client
// of a `crush server` that a second client may join, but Togo cannot start
// it that way yet (see the adapter file), so this adapter only lists
// sessions and applies changes by relaunching; the terminal shows the rest.
package crush

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed crush.yaml
var adapterFile []byte

func init() {
	harness.RegisterAdapterFile("crush", adapterFile)
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
