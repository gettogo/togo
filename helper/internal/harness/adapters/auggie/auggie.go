// Package auggie is the Auggie (Augment CLI) adapter: its adapter file and
// the auggie.hooks mapper. Auggie writes no live transcript, so the
// conversation is built from its documented hooks (PromptSubmit,
// PreToolUse, PostToolUse, Notification, Stop, SessionStart, SessionEnd),
// which `togo hook auggie <event>` forwards once the owner adds them to
// ~/.augment/settings.json.
package auggie

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed auggie.yaml
var adapterFile []byte

// MapperHooks is the mapper name the adapter file uses.
const MapperHooks = "auggie.hooks"

func init() {
	harness.RegisterAdapterFile("auggie", adapterFile)
	harness.RegisterMapper(MapperHooks, func(env harness.Env) harness.Mapper { return NewHooks(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
