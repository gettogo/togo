// Package cursor is the Cursor CLI harness adapter. The owner's build
// (2025.10) predates the CLI's ACP and model commands, so the version gate
// keeps it terminal-only until the owner updates.
package cursor

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed cursor.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("cursor", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
