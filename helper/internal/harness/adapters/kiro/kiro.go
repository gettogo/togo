// Package kiro is the Kiro CLI harness adapter. Kiro was not installed (its
// macOS installer is an app bundle), so nothing here is verified and the
// adapter is terminal-only.
package kiro

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed kiro.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("kiro", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
