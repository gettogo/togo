// Package auggie is the Auggie (Augment CLI) adapter: terminal-only by owner
// decision, so it declares no capabilities and maps no feed. The shared
// screen watcher still raises its dialogs on the phone.
package auggie

import (
	_ "embed"

	"github.com/gettogo/togo/helper/internal/harness"
)

//go:embed auggie.yaml
var adapterFile []byte

func init() { harness.RegisterAdapterFile("auggie", adapterFile) }

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }
