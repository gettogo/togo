// Package cline is the Cline CLI harness adapter: sessions from the
// documented `cline history --json` and relaunch with --id, -m, --thinking
// and --plan. The owner's 3.0.62 hangs on --help and --version, so nothing
// here was run against it.
package cline

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed cline.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("cline", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
