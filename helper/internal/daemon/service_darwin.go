package daemon

import "context"

// DefaultManager is the per-user service manager of this OS.
const DefaultManager = Launchd

// afterInstall has nothing to do: a LaunchAgent survives logout on its own.
func (Service) afterInstall(context.Context) {}
