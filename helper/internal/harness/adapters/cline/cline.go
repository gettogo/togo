// Package cline is the Cline CLI harness adapter: sessions from the
// documented `cline history --json`, relaunch with --id, and the
// cline.messages mapper, which reads the session messages file Cline 3.0
// keeps under ~/.cline/data/sessions/<id>/.
package cline

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed cline.yaml
var adapterFile []byte

func init() {
	harness.RegisterAdapterFile("cline", adapterFile)
	harness.RegisterMapper(MapperMessages, func(env harness.Env) harness.Mapper { return newMessages(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
