package daemon

import (
	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/watch"
)

// The watcher corrects a stale "working" only through a sink it can read
// back; the hub is that sink.
var _ watch.StatusReader = (*hub.Hub)(nil)
