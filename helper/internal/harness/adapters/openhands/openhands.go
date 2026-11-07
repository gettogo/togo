// Package openhands is the OpenHands CLI harness adapter. The project is no
// longer maintained and no conversation was created to check its files, so
// the adapter is terminal-only.
package openhands

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed openhands.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("openhands", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
