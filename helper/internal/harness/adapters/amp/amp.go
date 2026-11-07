// Package amp is the Amp harness adapter. Amp keeps its threads on
// ampcode.com and every thread command needs a login, so Amp is
// terminal-only: the file only names the harness and its version gate.
package amp

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed amp.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("amp", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
