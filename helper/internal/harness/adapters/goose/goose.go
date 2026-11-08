// Package goose is the Goose harness adapter: sessions from the documented
// `goose session list --format json`, typed /mode and /model commands, and
// the goose.db mapper, which reads the chat out of Goose's own session
// database (~/.local/share/goose/sessions/sessions.db) read-only.
package goose

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed goose.yaml
var adapterFile []byte

// MapperDB is the mapper name goose.yaml uses.
const MapperDB = "goose.db"

func init() {
	harness.RegisterAdapterFile("goose", adapterFile)
	harness.RegisterMapper(MapperDB, func(env harness.Env) harness.Mapper { return newDB(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
