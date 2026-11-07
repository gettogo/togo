// Package goose is the Goose harness adapter: sessions from the documented
// `goose session list --format json`, and typed /mode and /model commands.
// Goose has no hooks and no live chat feed the TUI shares, so the chat stays
// in the terminal.
package goose

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed goose.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("goose", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
