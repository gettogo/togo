// Package kimi is the Kimi Code harness adapter: its adapter file and the
// kimi.wire mapper, which reads the main agent's wire log
// (~/.kimi-code/sessions/wd_<name>_<hash>/session_<id>/agents/main/wire.jsonl).
// Kimi has no attach protocol for its TUI, so changes go back through
// relaunch flags and typed commands.
package kimi

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed kimi.yaml
var adapterFile []byte

// MapperWire is the mapper name the adapter file uses.
const MapperWire = "kimi.wire"

func init() {
	harness.RegisterAdapterFile("kimi", adapterFile)
	harness.RegisterMapper(MapperWire, func(env harness.Env) harness.Mapper { return NewWire(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
