// Package pi is the Pi coding agent adapter. Pi writes session JSONL v3,
// read by the shared pisession mapper, and has no attach protocol, so
// changes go through typed commands and relaunch flags.
package pi

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/pisession"
)

//go:embed pi.yaml
var adapterFile []byte

// MapperSession is the mapper name the adapter file uses.
const MapperSession = "pi.session"

func init() {
	harness.RegisterAdapterFile("pi", adapterFile)
	harness.RegisterMapper(MapperSession, func(env harness.Env) harness.Mapper { return pisession.New(MapperSession, env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
