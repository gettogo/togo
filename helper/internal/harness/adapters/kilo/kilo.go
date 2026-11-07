// Package kilo is the Kilo Code harness adapter. Kilo is an OpenCode fork
// whose TUI serves the same HTTP API and event stream, so the adapter is
// only its own adapter file: it reads the stream with the opencode.events
// mapper, which the opencode package registers.
package kilo

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
)

//go:embed kilo.yaml
var adapterFile []byte

// Mapper is the event mapper the adapter file names.
const Mapper = opencode.MapperEvents

func init() {
	harness.RegisterAdapterFile("kilo", adapterFile)
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
