package watch

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/wire"
)

// shellMark hands a shell command mark to the harness feeds. It changes no
// session status.
func (w *Watcher) shellMark(sig wire.Signal) error {
	if w.cfg.Shell != nil && json.Valid([]byte(sig.Body)) {
		w.cfg.Shell(sig.SessionID, []byte(sig.Body))
	}
	return nil
}
