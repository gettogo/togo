// Package amp is the Amp harness adapter. Amp keeps its threads on
// ampcode.com and writes no local transcript, so the chat comes from
// Togo's Amp plugin, which forwards Amp's plugin events to
// `togo hook amp <event>`; the shared hookstyle reader maps them with the
// amp dialect.
package amp

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

//go:embed amp.yaml
var adapterFile []byte

// MapperHooks is the hooks mapper name amp.yaml uses.
const MapperHooks = "amp.hooks"

func init() {
	harness.RegisterAdapterFile("amp", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks})
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
